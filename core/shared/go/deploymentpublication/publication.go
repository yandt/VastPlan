// Package deploymentpublication defines the narrow trusted boundary used by
// deployment-manager. The plugin can submit only an Application Composition;
// immutable platform profiles, artifact verification and control-plane KV stay
// behind the kernel service.
package deploymentpublication

import (
	"context"
	"errors"
	"strings"
	"time"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

const (
	DeploymentManagerPluginID = "cn.vastplan.platform.infrastructure.deployment-manager"
	KernelTargetsService      = "kernel.deployment.targets"
	KernelPreviewService      = "kernel.deployment.preview"
	KernelPublishService      = "kernel.deployment.publish"
	KernelReadinessService    = "kernel.deployment.readiness"
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
	Deployment            deploymentv2.Deployment      `json:"deployment"`
	Digest                string                       `json:"digest"`
	PlatformCatalogDigest string                       `json:"platformCatalogDigest"`
	PlatformProfile       compositioncommonv1.Ref      `json:"platformProfile"`
	KVRevision            uint64                       `json:"kvRevision,omitempty"`
	ArtifactReferences    []pluginv1.ArtifactReference `json:"artifactReferences"`
	ConfigurationCatalog  pluginconfiguration.Catalog  `json:"configurationCatalog"`
}

type ReadinessStatus string

const (
	ReadinessPending        ReadinessStatus = "Pending"
	ReadinessBlocked        ReadinessStatus = "Blocked"
	ReadinessReady          ReadinessStatus = "Ready"
	ReadinessDegraded       ReadinessStatus = "Degraded"
	ReadinessDependencyLost ReadinessStatus = "DependencyLost"
	ReadinessFailed         ReadinessStatus = "Failed"
	ReadinessStopped        ReadinessStatus = "Stopped"
)

type ReadinessUnit struct {
	ID               string          `json:"id"`
	Status           ReadinessStatus `json:"status"`
	DesiredReplicas  int             `json:"desired_replicas"`
	Replicas         int             `json:"replicas"`
	ReadyReplicas    int             `json:"ready_replicas"`
	DependencyIssues []string        `json:"dependency_issues,omitempty"`
	Errors           []string        `json:"errors,omitempty"`
}

type ReadinessObservation struct {
	SchemaVersion int             `json:"schema_version"`
	Tenant        string          `json:"tenant,omitempty"`
	Deployment    string          `json:"deployment"`
	Revision      uint64          `json:"revision"`
	Generation    uint64          `json:"generation"`
	Units         []ReadinessUnit `json:"units"`
	Status        ReadinessStatus `json:"status"`
	Reason        string          `json:"reason,omitempty"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (o ReadinessObservation) Validate() error {
	if o.SchemaVersion != 1 || strings.TrimSpace(o.Tenant) == "" || strings.TrimSpace(o.Deployment) == "" || o.Revision == 0 || o.UpdatedAt.IsZero() {
		return errors.New("部署 readiness observation 身份无效")
	}
	switch o.Status {
	case ReadinessPending, ReadinessBlocked, ReadinessReady, ReadinessDegraded, ReadinessDependencyLost, ReadinessFailed, ReadinessStopped:
	default:
		return errors.New("部署 readiness observation 状态无效")
	}
	return nil
}

type ReadinessRequest struct {
	DeploymentName     string `json:"deploymentName"`
	DeploymentRevision uint64 `json:"deploymentRevision"`
}

type ReadinessObserver interface {
	Observe(context.Context, string, string, uint64) (ReadinessObservation, error)
}

type Controller interface {
	Targets(context.Context, string) ([]Target, error)
	Preview(context.Context, string, backendcompositionv1.ApplicationComposition, uint64) (Result, error)
	Publish(context.Context, string, backendcompositionv1.ApplicationComposition, uint64, string) (Result, error)
}
