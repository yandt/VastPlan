// Package controlplanecommand 实现 Backend 内核的 controlplane 生产子命令。
package controlplanecommand

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/compositionresolver"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/configurationcatalog"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentcontroller"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentpublisher"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/platformcatalog"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
)

// Run 初始化 NATS KV、发布部署规格，并可持续运行多节点 assignment 控制器。
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("controlplane", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := bindControlPlaneFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	defaultControllerID(options.controllerID)
	if (*options.platformProfilePath == "") != (*options.applicationPath == "") {
		return errors.New("发布服务配置必须同时提供 -platform-profile 与 -application-composition")
	}
	publish := *options.platformProfilePath != ""
	if !publish && !*options.controllerMode {
		return errors.New("发布模式必须提供 -platform-profile 与 -application-composition")
	}
	if !publish && *options.controllerMode && *options.key == "" && *options.backendCatalogPath == "" {
		return errors.New("仅运行 controller 时必须提供 v2 部署 -options.key 或 -backend-platform-catalog")
	}
	if publish && *options.deploymentRevision == 0 {
		return errors.New("发布服务配置必须提供大于 0 的 -deployment-revision")
	}
	capacityPolicy := sharedstate.CapacityPolicy{}
	if *options.bootstrap {
		resolved, resolveErr := sharedstate.ResolveCapacityPolicy(*options.sharedStateMaxBytes, *options.sharedStateWarningPercent, *options.sharedStateCriticalPercent, *options.natsAllowInsecure)
		if resolveErr != nil {
			return resolveErr
		}
		capacityPolicy = resolved
	}

	artifacts, err := pluginservice.NewRepository(*options.repositoryRoot)
	if err != nil {
		return err
	}
	if *options.repositoryURL != "" && *options.repositoryToken == "" {
		*options.repositoryToken = os.Getenv("VASTPLAN_ARTIFACT_READ_TOKEN")
	}
	controllerArtifacts, err := buildControllerArtifactReader(artifacts, controllerRepositoryOptions{
		URL: *options.repositoryURL, ProfileFile: *options.repositoryProfile, TrustFile: *options.repositoryTrust,
		Token: *options.repositoryToken, TokenFile: *options.repositoryTokenFile, CAFile: *options.repositoryCA,
	})
	if err != nil {
		return err
	}
	var backendCatalog backendcompositionv1.BackendPlatformCatalog
	if *options.backendCatalogPath != "" {
		backendCatalog, err = backendcompositionv1.ParseBackendPlatformCatalogFile(*options.backendCatalogPath)
		if err != nil {
			return err
		}
		if !*options.controllerMode {
			return errors.New("-backend-platform-catalog 只用于 controller fleet 模式")
		}
	}
	var publicationApplication backendcompositionv1.ApplicationComposition
	var publicationCatalog backendcompositionv1.BackendPlatformCatalog
	if publish {
		profile, err := backendcompositionv1.ParsePlatformProfileFile(*options.platformProfilePath)
		if err != nil {
			return err
		}
		application, err := backendcompositionv1.ParseApplicationCompositionFile(*options.applicationPath)
		if err != nil {
			return err
		}
		publicationCatalog, err = deploymentpublisher.SeedCatalog(profile, application)
		if err != nil {
			return err
		}
		publicationApplication = application
	}
	nc, err := sharedcontrolplane.ConnectWithConfig(sharedcontrolplane.ConnectionConfig{
		URL: *options.natsURL, ClientName: "vastplan-controlplane",
		CAFile: *options.natsCA, CertFile: *options.natsCert, KeyFile: *options.natsKey, SeedFile: *options.natsSeed,
		Insecure: *options.natsAllowInsecure,
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
	if *options.bootstrap {
		buckets, err = sharedcontrolplane.EnsureBucketsWithOptions(openCtx, js, sharedcontrolplane.EnsureBucketsOptions{
			Replicas: *options.replicas, Storage: jetstream.FileStorage, SharedStateCapacity: capacityPolicy,
		})
	} else {
		buckets, err = sharedcontrolplane.OpenBuckets(openCtx, js)
	}
	if err != nil {
		return err
	}
	if *options.backendCatalogPath != "" && *options.bootstrap {
		catalogStore, err := platformcatalog.NewStore(buckets.BackendPlatformCatalogs, backendCatalog)
		if err != nil {
			return err
		}
		if _, err := catalogStore.Seed(openCtx); err != nil {
			return fmt.Errorf("持久 Backend Platform Catalog Seed: %w", err)
		}
	}
	if publish {
		derivedKey := sharedcontrolplane.DeploymentKey(publicationApplication.Metadata.Tenant, publicationApplication.Metadata.Name)
		if *options.key != "" && *options.key != derivedKey {
			return fmt.Errorf("种子配置目标与显式 Deployment options.key 不一致: expected=%s actual=%s", derivedKey, *options.key)
		}
		publisher, err := deploymentpublisher.New(
			publicationCatalog,
			controllerArtifacts,
			deploymentpublisher.KVApplier{KV: buckets.Deployments},
			configurationcatalog.Store{KV: buckets.Deployments},
			compositionresolver.Options{AllowDevelopmentPlugins: *options.allowDevelopmentPlugins},
			compositionresolver.Resolve,
		)
		if err != nil {
			return fmt.Errorf("创建统一服务发布器: %w", err)
		}
		preview, err := publisher.Preview(openCtx, publicationApplication.Metadata.Tenant, publicationApplication, *options.deploymentRevision)
		if err != nil {
			return fmt.Errorf("预览种子服务配置: %w", err)
		}
		result, err := publisher.Publish(openCtx, publicationApplication.Metadata.Tenant, publicationApplication, *options.deploymentRevision, preview.Digest)
		if err != nil {
			return fmt.Errorf("发布种子服务配置: %w", err)
		}
		if _, err := fmt.Fprintf(stdout, "已通过统一服务发布器发布 Deployment %s revision=%d kv-revision=%d source=seed-file options.key=%s\n", result.Deployment.Metadata.Name, result.Deployment.Revision, result.KVRevision, derivedKey); err != nil {
			return err
		}
	}
	if !*options.controllerMode {
		return nil
	}
	controller := deploymentcontroller.Controller{
		Deployments: buckets.Deployments,
		Scheduler: deploymentcontroller.Scheduler{
			Nodes: buckets.Nodes, Assignments: buckets.Assignments, Metrics: buckets.Autoscaling,
			Actual: buckets.Actual, Compositions: buckets.Compositions, Artifacts: controllerArtifacts,
		},
		Leaders: buckets.Controllers, Identity: *options.controllerID,
		Logf: func(format string, values ...any) { _, _ = fmt.Fprintf(stderr, format+"\n", values...) },
	}
	keys := make([]string, 0, len(backendCatalog.Bindings)+1)
	if *options.key != "" {
		keys = append(keys, *options.key)
	}
	for _, binding := range backendCatalog.Bindings {
		keys = append(keys, sharedcontrolplane.DeploymentKey(binding.TenantID, binding.DeploymentName))
	}
	if err := runControllerFleet(ctx, controller, keys); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("controller 退出: %w", err)
	}
	return nil
}

func runControllerFleet(ctx context.Context, template deploymentcontroller.Controller, keys []string) error {
	unique := map[string]struct{}{}
	for _, key := range keys {
		if key != "" {
			unique[key] = struct{}{}
		}
	}
	keys = keys[:0]
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return errors.New("controller fleet 没有部署目标")
	}
	controllerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsOut := make(chan error, len(keys))
	var workers sync.WaitGroup
	for i, key := range keys {
		controller := template
		controller.DeploymentKey = key
		controller.Identity = fmt.Sprintf("%s-%d", template.Identity, i+1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			errorsOut <- controller.Run(controllerCtx)
		}()
	}
	first := <-errorsOut
	cancel()
	workers.Wait()
	if errors.Is(first, context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	return first
}
