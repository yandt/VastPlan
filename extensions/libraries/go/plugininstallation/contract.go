// Package plugininstallation defines the language-neutral application-plugin
// installation intent shared by central control, service self-service and the
// development automation source. It describes a desired change and a preview;
// it never grants repository or deployment authority by itself.
package plugininstallation

import (
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const (
	ProtocolVersion             = 1
	PreviewOperation            = "previewPluginInstallation"
	SelfServicePreviewOperation = "previewSelfServicePluginInstallation"
	DevelopmentPreviewOperation = "previewDevelopmentPluginInstallation"

	SourceController  Source = "controller"
	SourceSelfService Source = "self-service"
	SourceDevelopment Source = "development"

	ActionInstall Action = "install"
	ActionUpgrade Action = "upgrade"
	ActionRemove  Action = "remove"

	StatusResolved           Status = "Resolved"
	StatusNeedsConfiguration Status = "NeedsConfiguration"
	StatusInvalid            Status = "Invalid"

	PackageAdded   PackageChangeKind = "Added"
	PackageUpdated PackageChangeKind = "Updated"
	PackageRemoved PackageChangeKind = "Removed"

	ApplyServiceGeneration ApplyStrategy = "service-generation"
)

type Source string
type Action string
type Status string
type PackageChangeKind string
type ApplyStrategy string

// Target identifies a logical service, not a physical node. Cluster rollout is
// derived later from the resulting Deployment revision.
type Target struct {
	Kernel     string `json:"kernel"`
	Deployment string `json:"deployment"`
	UnitID     string `json:"unitId"`
}

// Change contains only an application root-plugin mutation. Transitive
// dependencies remain resolver-owned and are reported read-only in Preview.
type Change struct {
	Action      Action                        `json:"action"`
	PluginID    string                        `json:"pluginId"`
	Requirement *pluginv1.ArtifactRequirement `json:"requirement,omitempty"`
}

// PreviewRequest intentionally omits Source. A trusted entry adapter selects
// one source protocol from the invoked operation and injects the strategy into
// the shared workflow.
type PreviewRequest struct {
	Version                int    `json:"version"`
	Target                 Target `json:"target"`
	Change                 Change `json:"change"`
	ExpectedActiveRevision uint64 `json:"expectedActiveRevision,omitempty"`
}

type PackageChange struct {
	Kind     PackageChangeKind             `json:"kind"`
	PluginID string                        `json:"pluginId"`
	Root     bool                          `json:"root"`
	Before   *pluginv1.ArtifactLockPackage `json:"before,omitempty"`
	After    *pluginv1.ArtifactLockPackage `json:"after,omitempty"`
}

type ConfigurationGap struct {
	UnitID   string                                          `json:"unitId"`
	PluginID string                                          `json:"pluginId"`
	Missing  []backendcompositionv1.ConfigurationRequirement `json:"missing"`
}

type Impact struct {
	ApplyStrategy         ApplyStrategy `json:"applyStrategy"`
	RequiresApproval      bool          `json:"requiresApproval"`
	KernelRestartRequired bool          `json:"kernelRestartRequired"`
	RootChanged           bool          `json:"rootChanged"`
	Noop                  bool          `json:"noop"`
}

// Preview is side-effect free. PlanDigest, repository revision, Platform
// Profile and exact ArtifactLock must be rebound when a later candidate is
// submitted; any drift makes the preview stale.
type Preview struct {
	Version               int                                         `json:"version"`
	Source                Source                                      `json:"source"`
	Status                Status                                      `json:"status"`
	Target                Target                                      `json:"target"`
	Action                Action                                      `json:"action"`
	PluginID              string                                      `json:"pluginId"`
	ActiveRevision        uint64                                      `json:"activeRevision"`
	CandidateRevision     uint64                                      `json:"candidateRevision"`
	CandidateIntentDigest string                                      `json:"candidateIntentDigest"`
	PlanDigest            string                                      `json:"planDigest"`
	PreviewDigest         string                                      `json:"previewDigest,omitempty"`
	RepositoryRevision    uint64                                      `json:"repositoryRevision,omitempty"`
	PlatformProfile       compositioncommonv1.Ref                     `json:"platformProfile"`
	ArtifactLock          *pluginv1.ArtifactLock                      `json:"artifactLock,omitempty"`
	Changes               []PackageChange                             `json:"changes"`
	ConfigurationGaps     []ConfigurationGap                          `json:"configurationGaps"`
	Diagnostics           []backendcompositionv1.ResolutionDiagnostic `json:"diagnostics"`
	Impact                Impact                                      `json:"impact"`
}
