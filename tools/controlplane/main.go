// controlplane 初始化 NATS KV、发布部署规格，并可运行多节点 assignment 控制器。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/kernels/backend/deploymentcontroller"
	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
)

func main() {
	natsURL := flag.String("nats-url", "tls://127.0.0.1:4222", "NATS URL")
	natsCA := flag.String("nats-ca", "", "NATS CA PEM")
	natsCert := flag.String("nats-cert", "", "NATS mTLS 客户端证书 PEM")
	natsKey := flag.String("nats-key", "", "NATS mTLS 客户端私钥 PEM")
	natsSeed := flag.String("nats-seed", "", "bootstrap 或 controller 角色 NKey seed（0600）")
	natsAllowInsecure := flag.Bool("nats-allow-insecure", false, "仅本地开发：允许明文匿名 NATS")
	desiredPath := flag.String("desired", "", "要发布的 DesiredState v1 或 Deployment v2 JSON")
	key := flag.String("key", "", "KV key；默认从 metadata.tenant/name 生成")
	controllerMode := flag.Bool("controller", false, "持续 watch v2 部署与节点租约并生成每节点 assignment")
	controllerID := flag.String("controller-id", "", "controller 选主身份；默认 hostname-pid")
	bootstrap := flag.Bool("bootstrap", false, "创建/校准控制面 bucket")
	replicas := flag.Int("replicas", 1, "bootstrap 时的 JetStream 副本数；生产建议至少 3")
	flag.Parse()
	if *controllerID == "" {
		hostname, _ := os.Hostname()
		*controllerID = fmt.Sprintf("%s-%d", hostname, os.Getpid())
	}
	if *desiredPath == "" && !*controllerMode {
		fmt.Fprintln(os.Stderr, "发布模式必须提供 -desired")
		os.Exit(2)
	}
	if *desiredPath == "" && *key == "" {
		fmt.Fprintln(os.Stderr, "仅运行 controller 时必须提供 v2 部署 -key")
		os.Exit(2)
	}
	var raw []byte
	version := 2
	if *desiredPath != "" {
		var err error
		raw, err = os.ReadFile(*desiredPath)
		if err != nil {
			fatalf("读取部署规格: %v", err)
		}
		var header struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			fatalf("读取部署规格版本: %v", err)
		}
		version = header.Version
	}
	nc, err := controlplane.ConnectWithConfig(controlplane.ConnectionConfig{
		URL: *natsURL, ClientName: "vastplan-controlplane",
		CAFile: *natsCA, CertFile: *natsCert, KeyFile: *natsKey, SeedFile: *natsSeed,
		Insecure: *natsAllowInsecure,
		Logf:     func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) },
	})
	if err != nil {
		fatalf("%v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		fatalf("创建 JetStream 客户端: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var buckets controlplane.Buckets
	if *bootstrap {
		buckets, err = controlplane.EnsureBuckets(ctx, js, *replicas, jetstream.FileStorage)
	} else {
		buckets, err = controlplane.OpenBuckets(ctx, js)
	}
	if err != nil {
		fatalf("%v", err)
	}
	if len(raw) > 0 {
		switch version {
		case 1:
			state, parseErr := deploymentv1.Parse(raw)
			if parseErr != nil {
				fatalf("DesiredState v1 无效: %v", parseErr)
			}
			if *controllerMode {
				fatalf("controller 只接受全局 deployment v2")
			}
			if *key == "" {
				*key = controlplane.DesiredKey(state.Metadata.Tenant, state.Metadata.Name)
			}
			kvRevision, applied, applyErr := controlplane.ApplyDesiredState(ctx, buckets.Desired, *key, raw)
			if applyErr != nil {
				fatalf("发布期望态: %v", applyErr)
			}
			fmt.Printf("已发布 DesiredState %s revision=%d kv-revision=%d key=%s\n", applied.Metadata.Name, applied.Revision, kvRevision, *key)
		case 2:
			deployment, parseErr := deploymentv2.Parse(raw)
			if parseErr != nil {
				fatalf("Deployment v2 无效: %v", parseErr)
			}
			if *key == "" {
				*key = controlplane.DeploymentKey(deployment.Metadata.Tenant, deployment.Metadata.Name)
			}
			kvRevision, applied, applyErr := controlplane.ApplyDeployment(ctx, buckets.Deployments, *key, raw)
			if applyErr != nil {
				fatalf("发布集群部署: %v", applyErr)
			}
			fmt.Printf("已发布 Deployment %s revision=%d kv-revision=%d key=%s\n", applied.Metadata.Name, applied.Revision, kvRevision, *key)
		default:
			fatalf("不支持 deployment version=%d", version)
		}
	}
	if *controllerMode {
		runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		controller := deploymentcontroller.Controller{
			Deployments: buckets.Deployments, DeploymentKey: *key,
			Scheduler: deploymentcontroller.Scheduler{Nodes: buckets.Nodes, Assignments: buckets.Assignments, Metrics: buckets.Autoscaling},
			Leaders:   buckets.Controllers, Identity: *controllerID,
			Logf: func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) },
		}
		if err := controller.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			fatalf("controller 退出: %v", err)
		}
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
