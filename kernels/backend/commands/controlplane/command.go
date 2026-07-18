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

	"cdsoft.com.cn/VastPlan/kernels/backend/compositionresolver"
	"cdsoft.com.cn/VastPlan/kernels/backend/deploymentcontroller"
	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/schemas/composition/backend/v1"
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
	platformProfilePath := flags.String("platform-profile", "", "平台管理员发布的 Platform Profile v1 JSON")
	applicationPath := flags.String("application-composition", "", "应用配置人员发布的 Application Composition v1 JSON")
	deploymentRevision := flags.Uint64("deployment-revision", 0, "Resolver 输出的独立单调 Deployment revision")
	allowDevelopmentPlugins := flags.Bool("allow-development-plugins", false, "仅本地开发：允许 example 或历史未分类首方插件")
	key := flags.String("key", "", "KV key；默认从 metadata.tenant/name 生成")
	controllerMode := flags.Bool("controller", false, "持续 watch v2 部署与节点租约并生成每节点 assignment")
	controllerID := flags.String("controller-id", "", "controller 选主身份；默认 hostname-pid")
	repositoryRoot := flags.String("repository", ".vastplan/repository", "controller 读取完整 manifest 的本地不可变制品仓库")
	bootstrap := flags.Bool("bootstrap", false, "创建/校准控制面 bucket")
	replicas := flags.Int("replicas", 1, "bootstrap 时的 JetStream 副本数；生产建议至少 3")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *controllerID == "" {
		hostname, _ := os.Hostname()
		*controllerID = fmt.Sprintf("%s-%d", hostname, os.Getpid())
	}
	if (*platformProfilePath == "") != (*applicationPath == "") {
		return errors.New("发布服务配置必须同时提供 -platform-profile 与 -application-composition")
	}
	publish := *platformProfilePath != ""
	if !publish && !*controllerMode {
		return errors.New("发布模式必须提供 -platform-profile 与 -application-composition")
	}
	if !publish && *key == "" {
		return errors.New("仅运行 controller 时必须提供 v2 部署 -key")
	}
	if publish && *deploymentRevision == 0 {
		return errors.New("发布服务配置必须提供大于 0 的 -deployment-revision")
	}

	artifacts, err := pluginservice.NewRepository(*repositoryRoot)
	if err != nil {
		return err
	}
	var raw []byte
	if publish {
		profile, err := backendcompositionv1.ParsePlatformProfileFile(*platformProfilePath)
		if err != nil {
			return err
		}
		application, err := backendcompositionv1.ParseApplicationCompositionFile(*applicationPath)
		if err != nil {
			return err
		}
		resolved, err := compositionresolver.Resolve(profile, application, *deploymentRevision, artifacts, compositionresolver.Options{AllowDevelopmentPlugins: *allowDevelopmentPlugins})
		if err != nil {
			return fmt.Errorf("解析平台与应用组合: %w", err)
		}
		raw, err = json.Marshal(resolved)
		if err != nil {
			return fmt.Errorf("编码解析后的 Deployment: %w", err)
		}
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
		if err := publishDeployment(openCtx, stdout, buckets, key, raw); err != nil {
			return err
		}
	}
	if !*controllerMode {
		return nil
	}
	controller := deploymentcontroller.Controller{
		Deployments: buckets.Deployments, DeploymentKey: *key,
		Scheduler: deploymentcontroller.Scheduler{
			Nodes: buckets.Nodes, Assignments: buckets.Assignments, Metrics: buckets.Autoscaling,
			Actual: buckets.Actual, Compositions: buckets.Compositions, Artifacts: artifacts,
		},
		Leaders: buckets.Controllers, Identity: *controllerID,
		Logf: func(format string, values ...any) { _, _ = fmt.Fprintf(stderr, format+"\n", values...) },
	}
	if err := controller.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("controller 退出: %w", err)
	}
	return nil
}

func publishDeployment(ctx context.Context, stdout io.Writer, buckets sharedcontrolplane.Buckets, key *string, raw []byte) error {
	deployment, err := deploymentv2.Parse(raw)
	if err != nil {
		return fmt.Errorf("Resolver 生成的 deployment v2 无效: %w", err)
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
	return nil
}
