// Package deploymentpublication defines the narrow trusted boundary used by
// deployment-manager. The plugin can submit only an Application Composition;
// immutable platform profiles, artifact verification and control-plane KV stay
// behind the kernel service.
package deploymentpublication

import (
	"context"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
)

const (
	DeploymentManagerPluginID = "cn.vastplan.platform.infrastructure.deployment-manager"
	KernelTargetsService      = "kernel.deployment.targets"
	KernelPreviewService      = "kernel.deployment.preview"
	KernelPublishService      = "kernel.deployment.publish"
)

type Target struct {
	DeploymentName  string                  `json:"deploymentName"`
	PlatformProfile compositioncommonv1.Ref `json:"platformProfile"`
}

type PreviewRequest struct {
	Composition        backendcompositionv1.ApplicationComposition `json:"composition"`
	DeploymentRevision uint64                                      `json:"deploymentRevision"`
}

type PublishRequest struct {
	Composition        backendcompositionv1.ApplicationComposition `json:"composition"`
	DeploymentRevision uint64                                      `json:"deploymentRevision"`
	ExpectedDigest     string                                      `json:"expectedDigest"`
}

type Result struct {
	Deployment deploymentv2.Deployment `json:"deployment"`
	Digest     string                  `json:"digest"`
	KVRevision uint64                  `json:"kvRevision,omitempty"`
}

type Controller interface {
	Targets(context.Context, string) ([]Target, error)
	Preview(context.Context, string, backendcompositionv1.ApplicationComposition, uint64) (Result, error)
	Publish(context.Context, string, backendcompositionv1.ApplicationComposition, uint64, string) (Result, error)
}
