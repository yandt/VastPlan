// Package controlplanecommand 实现 Backend 内核的 controlplane 生产子命令。
package controlplanecommand

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/kernels/backend/deploymentcontroller"
	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/shared/go/controlplane"
)

// Run 初始化 NATS KV、发布部署规格，并可持续运行多节点 assignment 控制器。
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("controlplane", flag.ContinueOnError)
	flags.SetOutput(stderr)
	natsURL := flags.String("nats-url", "tls://127.0.0.1:4222", "NATS URL")
	natsCA := flags.String("nats-ca", "", "NATS CA PEM")
	natsCert := flags.String("nats-cert", "", "NATS mTLS 客户端证书 PEM")
	natsKey := flags.String("nats-key", "", "NATS mTLS 客户端私钥 PEM")
	natsSeed := flags.String("nats-seed", "", "bootstrap 或 controller 角色 NKey seed（0600）")
	natsAllowInsecure := flags.Bool("nats-allow-insecure", false, "仅本地开发：允许明文匿名 NATS")
	desiredPath := flags.String("desired", "", "要发布的 DesiredState v1 或 Deployment v2 JSON")
	key := flags.String("key", "", "KV key；默认从 metadata.tenant/name 生成")
	controllerMode := flags.Bool("controller", false, "持续 watch v2 部署与节点租约并生成每节点 assignment")
	controllerID := flags.String("controller-id", "", "controller 选主身份；默认 hostname-pid")
	bootstrap := flags.Bool("bootstrap", false, "创建/校准控制面 bucket")
	replicas := flags.Int("replicas", 1, "bootstrap 时的 JetStream 副本数；生产建议至少 3")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *controllerID == "" {
		hostname, _ := os.Hostname()
		*controllerID = fmt.Sprintf("%s-%d", hostname, os.Getpid())
	}
	if *desiredPath == "" && !*controllerMode {
		return errors.New("发布模式必须提供 -desired")
	}
	if *desiredPath == "" && *key == "" {
		return errors.New("仅运行 controller 时必须提供 v2 部署 -key")
	}

	raw, version, err := readDeployment(*desiredPath)
	if err != nil {
		return err
	}
	nc, err := sharedcontrolplane.ConnectWithConfig(sharedcontrolplane.ConnectionConfig{
		URL: *natsURL, ClientName: "vastplan-controlplane",
		CAFile: *natsCA, CertFile: *natsCert, KeyFile: *natsKey, SeedFile: *natsSeed,
		Insecure: *natsAllowInsecure,
		Logf:     func(format string, values ...any) { _, _ = fmt.Fprintf(stderr, format+"\n", values...) },
	})
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("创建 JetStream 客户端: %w", err)
	}
	openCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var buckets sharedcontrolplane.Buckets
	if *bootstrap {
		buckets, err = sharedcontrolplane.EnsureBuckets(openCtx, js, *replicas, jetstream.FileStorage)
	} else {
		buckets, err = sharedcontrolplane.OpenBuckets(openCtx, js)
	}
	if err != nil {
		return err
	}
	if len(raw) > 0 {
		if err := publishDeployment(openCtx, stdout, buckets, key, version, raw, *controllerMode); err != nil {
			return err
		}
	}
	if !*controllerMode {
		return nil
	}
	controller := deploymentcontroller.Controller{
		Deployments: buckets.Deployments, DeploymentKey: *key,
		Scheduler: deploymentcontroller.Scheduler{Nodes: buckets.Nodes, Assignments: buckets.Assignments, Metrics: buckets.Autoscaling},
		Leaders:   buckets.Controllers, Identity: *controllerID,
		Logf: func(format string, values ...any) { _, _ = fmt.Fprintf(stderr, format+"\n", values...) },
	}
	if err := controller.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("controller 退出: %w", err)
	}
	return nil
}

func readDeployment(path string) ([]byte, int, error) {
	if path == "" {
		return nil, 2, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("读取部署规格: %w", err)
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, 0, fmt.Errorf("读取部署规格版本: %w", err)
	}
	return raw, header.Version, nil
}

func publishDeployment(ctx context.Context, stdout io.Writer, buckets sharedcontrolplane.Buckets, key *string, version int, raw []byte, controllerMode bool) error {
	switch version {
	case 1:
		state, err := deploymentv1.Parse(raw)
		if err != nil {
			return fmt.Errorf("desired state v1 无效: %w", err)
		}
		if controllerMode {
			return errors.New("controller 只接受全局 deployment v2")
		}
		if *key == "" {
			*key = sharedcontrolplane.DesiredKey(state.Metadata.Tenant, state.Metadata.Name)
		}
		kvRevision, applied, err := sharedcontrolplane.ApplyDesiredState(ctx, buckets.Desired, *key, raw)
		if err != nil {
			return fmt.Errorf("发布期望态: %w", err)
		}
		if _, err := fmt.Fprintf(stdout, "已发布 DesiredState %s revision=%d kv-revision=%d key=%s\n", applied.Metadata.Name, applied.Revision, kvRevision, *key); err != nil {
			return err
		}
	case 2:
		deployment, err := deploymentv2.Parse(raw)
		if err != nil {
			return fmt.Errorf("deployment v2 无效: %w", err)
		}
		if *key == "" {
			*key = sharedcontrolplane.DeploymentKey(deployment.Metadata.Tenant, deployment.Metadata.Name)
		}
		kvRevision, applied, err := sharedcontrolplane.ApplyDeployment(ctx, buckets.Deployments, *key, raw)
		if err != nil {
			return fmt.Errorf("发布集群部署: %w", err)
		}
		if _, err := fmt.Fprintf(stdout, "已发布 Deployment %s revision=%d kv-revision=%d key=%s\n", applied.Metadata.Name, applied.Revision, kvRevision, *key); err != nil {
			return err
		}
	default:
		return fmt.Errorf("不支持 deployment version=%d", version)
	}
	return nil
}
