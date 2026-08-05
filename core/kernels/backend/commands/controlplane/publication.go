package controlplanecommand

import (
	"context"
	"fmt"
	"io"
	"time"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/compositionresolver"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/configurationcatalog"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentcontroller"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/deploymentpublisher"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

// publishSeedDeployment keeps explicit Seed publication as one application
// workflow. Each potentially blocking phase owns its own deadline; catalog
// preparation must not consume the Preview or Publish budget.
func publishSeedDeployment(
	ctx context.Context,
	options controlPlaneFlagValues,
	catalog backendcompositionv1.BackendPlatformCatalog,
	application backendcompositionv1.ApplicationComposition,
	artifacts deploymentcontroller.ArtifactReader,
	buckets sharedcontrolplane.Buckets,
	stdout io.Writer,
) error {
	derivedKey := sharedcontrolplane.DeploymentKey(application.Metadata.Tenant, application.Metadata.Name)
	if *options.key != "" && *options.key != derivedKey {
		return fmt.Errorf("种子配置目标与显式 Deployment options.key 不一致: expected=%s actual=%s", derivedKey, *options.key)
	}
	publisher, err := deploymentpublisher.New(
		catalog,
		artifacts,
		deploymentpublisher.KVApplier{KV: buckets.Deployments},
		configurationcatalog.Store{KV: buckets.Deployments},
		compositionresolver.Options{AllowDevelopmentPlugins: *options.allowDevelopmentPlugins},
		compositionresolver.Resolve,
	)
	if err != nil {
		return fmt.Errorf("创建统一服务发布器: %w", err)
	}
	previewCtx, cancelPreview := context.WithTimeout(ctx, 15*time.Second)
	preview, err := publisher.Preview(previewCtx, application.Metadata.Tenant, application, *options.deploymentRevision)
	cancelPreview()
	if err != nil {
		return fmt.Errorf("预览种子服务配置: %w", err)
	}
	publishCtx, cancelPublish := context.WithTimeout(ctx, 15*time.Second)
	result, err := publisher.Publish(publishCtx, application.Metadata.Tenant, application, *options.deploymentRevision, preview.Digest)
	cancelPublish()
	if err != nil {
		return fmt.Errorf("发布种子服务配置: %w", err)
	}
	_, err = fmt.Fprintf(stdout, "已通过统一服务发布器发布 Deployment %s revision=%d kv-revision=%d source=seed-file options.key=%s\n", result.Deployment.Metadata.Name, result.Deployment.Revision, result.KVRevision, derivedKey)
	return err
}
