// Package plugininstallation defines the language-neutral application-plugin
// installation intent shared by central control, service self-service and the
// development automation source. It describes a desired change and a preview;
// it never grants repository or deployment authority by itself.
package plugininstallation

import (
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
)

const (
	ProtocolVersion              = 1
	PreviewOperation             = "previewPluginInstallation"
	SelfServicePreviewOperation  = "previewSelfServicePluginInstallation"
	DevelopmentPreviewOperation  = "previewDevelopmentPluginInstallation"
	CreateOperation              = "createPluginInstallationCandidate"
	SelfServiceCreateOperation   = "createSelfServicePluginInstallationCandidate"
	DevelopmentCreateOperation   = "createDevelopmentPluginInstallationCandidate"
	ListTargetsOperation         = "listPluginInstallationTargets"
	ListOperation                = "listPluginInstallationCandidates"
	GetOperation                 = "getPluginInstallationCandidate"
	SubmitOperation              = "submitPluginInstallationCandidate"
	ApproveOperation             = "approvePluginInstallationCandidate"
	ActivateOperation            = "activatePluginInstallationCandidate"
	CancelOperation              = "cancelPluginInstallationCandidate"
	RollbackOperation            = "rollbackPluginInstallationCandidate"
	SelfServiceListOperation     = "listSelfServicePluginInstallationCandidates"
	SelfServiceGetOperation      = "getSelfServicePluginInstallationCandidate"
	SelfServiceSubmitOperation   = "submitSelfServicePluginInstallationCandidate"
	SelfServiceApproveOperation  = "approveSelfServicePluginInstallationCandidate"
	SelfServiceActivateOperation = "activateSelfServicePluginInstallationCandidate"
	SelfServiceCancelOperation   = "cancelSelfServicePluginInstallationCandidate"
	SelfServiceRollbackOperation = "rollbackSelfServicePluginInstallationCandidate"

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

	CandidatePlanned         CandidateStatus = "Planned"
	CandidatePendingApproval CandidateStatus = "PendingApproval"
	CandidateApproved        CandidateStatus = "Approved"
	CandidateActivating      CandidateStatus = "Activating"
	CandidateReady           CandidateStatus = "Ready"
	CandidateStale           CandidateStatus = "Stale"
	CandidateCancelled       CandidateStatus = "Cancelled"
	CandidateRolledBack      CandidateStatus = "RolledBack"
	CandidateSuperseded      CandidateStatus = "Superseded"
)

type Source string
type Action string
type Status string
type PackageChangeKind string
type ApplyStrategy string
type CandidateStatus string

// Target identifies a logical service, not a physical node. Cluster rollout is
// derived later from the resulting Deployment revision.
type Target struct {
	Kernel     string `json:"kernel"`
	Deployment string `json:"deployment"`
	UnitID     string `json:"unitId"`
}

// TargetOption is the minimum controller-facing projection required to bind a
// request to the current logical service revision. It intentionally excludes
// the full Application Intent and deployment history.
type TargetOption struct {
	Target         Target `json:"target"`
	ServiceClass   string `json:"serviceClass"`
	ActiveRevision uint64 `json:"activeRevision"`
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
	Approval              *approvalv2.Decision                        `json:"approval,omitempty"`
}

// Candidate is the durable lifecycle handle. ServiceRevisionID points to the
// existing deployment workflow; status is a projection of that revision, so
// installation never owns a second approval or publication state machine.
type Candidate struct {
	ID                        string                                      `json:"id"`
	Status                    CandidateStatus                             `json:"status"`
	Source                    Source                                      `json:"source"`
	Preview                   Preview                                     `json:"preview"`
	Rollout                   *deploymentpublication.ReadinessObservation `json:"rollout,omitempty"`
	ServiceRevisionID         uint64                                      `json:"serviceRevisionId"`
	PreviousServiceRevisionID uint64                                      `json:"previousServiceRevisionId"`
	RollbackServiceRevisionID uint64                                      `json:"rollbackServiceRevisionId,omitempty"`
	RequestedBy               string                                      `json:"requestedBy"`
	SubmittedBy               string                                      `json:"submittedBy,omitempty"`
	ApprovedBy                string                                      `json:"approvedBy,omitempty"`
	ActivatedBy               string                                      `json:"activatedBy,omitempty"`
	CancelledBy               string                                      `json:"cancelledBy,omitempty"`
	CreatedAt                 string                                      `json:"createdAt"`
	UpdatedAt                 string                                      `json:"updatedAt"`
}

type CandidateLookup struct {
	CandidateID string `json:"candidateId"`
}
