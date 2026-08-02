package platformadminapi

import (
	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfiguration"
)

type ManagedNode struct {
	ID        string             `json:"id"`
	Plan      nodebootstrap.Plan `json:"plan"`
	Version   int64              `json:"version"`
	CreatedAt string             `json:"createdAt"`
	UpdatedAt string             `json:"updatedAt"`
}

type PutManagedNodeRequest struct {
	Plan      nodebootstrap.Plan `json:"plan"`
	IfVersion *int64             `json:"ifVersion,omitempty"`
}

type BootstrapJobState string

const (
	BootstrapPending       BootstrapJobState = "Pending"
	BootstrapApproved      BootstrapJobState = "Approved"
	BootstrapConnecting    BootstrapJobState = "Connecting"
	BootstrapInstalling    BootstrapJobState = "Installing"
	BootstrapSystemdActive BootstrapJobState = "SystemdActive"
	BootstrapReady         BootstrapJobState = "Ready"
	BootstrapFailed        BootstrapJobState = "Failed"
	BootstrapExpired       BootstrapJobState = "Expired"
)

type BootstrapJob struct {
	ID          string            `json:"id"`
	NodeID      string            `json:"nodeId"`
	NodeVersion int64             `json:"nodeVersion"`
	State       BootstrapJobState `json:"state"`
	RequestedBy string            `json:"requestedBy"`
	ApprovedBy  string            `json:"approvedBy,omitempty"`
	ErrorCode   string            `json:"errorCode,omitempty"`
	CreatedAt   string            `json:"createdAt"`
	UpdatedAt   string            `json:"updatedAt"`
	ExpiresAt   string            `json:"expiresAt"`
}

type DeploymentTarget struct {
	DeploymentName  string                  `json:"deploymentName"`
	PlatformProfile compositioncommonv1.Ref `json:"platformProfile"`
}

type ServiceRevisionStatus string

const (
	ServiceDraft           ServiceRevisionStatus = "Draft"
	ServicePendingApproval ServiceRevisionStatus = "PendingApproval"
	ServiceApproved        ServiceRevisionStatus = "Approved"
	ServicePublishing      ServiceRevisionStatus = "Publishing"
	ServicePublished       ServiceRevisionStatus = "Published"
)

type ServiceRevision struct {
	ID                       uint64                                              `json:"id"`
	Deployment               string                                              `json:"deployment"`
	Status                   ServiceRevisionStatus                               `json:"status"`
	Active                   bool                                                `json:"active"`
	Intent                   *backendcompositionv1.ApplicationIntent             `json:"intent,omitempty"`
	ResolutionReport         *backendcompositionv1.ResolutionReport              `json:"resolutionReport,omitempty"`
	ConfigurationSnapshot    *backendcompositionv1.PlanningConfigurationSnapshot `json:"configurationSnapshot,omitempty"`
	PlanningStale            bool                                                `json:"planningStale,omitempty"`
	PlanningStaleReason      string                                              `json:"planningStaleReason,omitempty"`
	ObservedPlanDigest       string                                              `json:"observedPlanDigest,omitempty"`
	SubmittedPlanDigest      string                                              `json:"submittedPlanDigest,omitempty"`
	ApprovedPlanDigest       string                                              `json:"approvedPlanDigest,omitempty"`
	Composition              backendcompositionv1.ApplicationComposition         `json:"composition"`
	Preview                  deploymentv2.Deployment                             `json:"preview"`
	PreviewDigest            string                                              `json:"previewDigest"`
	ArtifactReferences       []pluginv1.ArtifactReference                        `json:"artifactReferences"`
	ConfigurationCatalog     pluginconfiguration.Catalog                         `json:"configurationCatalog"`
	ConfigurationCandidateID string                                              `json:"configurationCandidateId,omitempty"`
	ConfigurationID          string                                              `json:"configurationId,omitempty"`
	PreviousServiceRevision  uint64                                              `json:"previousServiceRevision,omitempty"`
	RollbackServiceRevision  uint64                                              `json:"rollbackServiceRevision,omitempty"`
	KVRevision               uint64                                              `json:"kvRevision,omitempty"`
	ReferencePending         bool                                                `json:"referencePending,omitempty"`
	SubmittedBy              string                                              `json:"submittedBy,omitempty"`
	ApprovedBy               string                                              `json:"approvedBy,omitempty"`
	PublishedBy              string                                              `json:"publishedBy,omitempty"`
	CreatedAt                string                                              `json:"createdAt"`
	UpdatedAt                string                                              `json:"updatedAt"`
}

type ServiceAuditEvent struct {
	ID            uint64 `json:"id"`
	RevisionID    uint64 `json:"revisionId"`
	Deployment    string `json:"deployment"`
	Action        string `json:"action"`
	ActorID       string `json:"actorId"`
	IntentDigest  string `json:"intentDigest,omitempty"`
	PlanDigest    string `json:"planDigest,omitempty"`
	PreviewDigest string `json:"previewDigest,omitempty"`
	At            string `json:"at"`
}

type TestTargetKind string

const TestTargetBackend TestTargetKind = "backend"

// TestTargetBinding is a durable pre-authorization. It identifies one
// application-owned plugin slot; it does not grant permission to edit a
// Platform Profile or introduce a new plugin into a service.
type TestTargetBinding struct {
	ID                string         `json:"id"`
	Kind              TestTargetKind `json:"kind"`
	Deployment        string         `json:"deployment"`
	UnitID            string         `json:"unitId"`
	PluginID          string         `json:"pluginId"`
	AllowInstall      bool           `json:"allowInstall,omitempty"`
	PortalTargets     []string       `json:"portalTargets"`
	AllowedPublishers []string       `json:"allowedPublishers"`
	Enabled           bool           `json:"enabled"`
	Version           int64          `json:"version"`
	CreatedAt         string         `json:"createdAt"`
	UpdatedAt         string         `json:"updatedAt"`
}

type PutTestTargetBindingRequest struct {
	Kind              TestTargetKind `json:"kind"`
	Deployment        string         `json:"deployment"`
	UnitID            string         `json:"unitId"`
	PluginID          string         `json:"pluginId"`
	AllowInstall      bool           `json:"allowInstall,omitempty"`
	PortalTargets     []string       `json:"portalTargets"`
	AllowedPublishers []string       `json:"allowedPublishers"`
	Enabled           bool           `json:"enabled"`
	IfVersion         *int64         `json:"ifVersion,omitempty"`
}

type TestReleaseStatus string

const (
	TestReleaseQueued      TestReleaseStatus = "Queued"
	TestReleaseResolving   TestReleaseStatus = "Resolving"
	TestReleasePreparing   TestReleaseStatus = "Preparing"
	TestReleaseValidating  TestReleaseStatus = "Validating"
	TestReleaseActivating  TestReleaseStatus = "Activating"
	TestReleaseReady       TestReleaseStatus = "Ready"
	TestReleaseRollingBack TestReleaseStatus = "RollingBack"
	TestReleaseRolledBack  TestReleaseStatus = "RolledBack"
	TestReleaseFailed      TestReleaseStatus = "Failed"
	TestReleaseSuperseded  TestReleaseStatus = "Superseded"
)

type TestRelease struct {
	ID                         uint64                       `json:"id"`
	BindingID                  string                       `json:"bindingId"`
	Receipt                    artifactrepositoryv1.Receipt `json:"receipt"`
	Status                     TestReleaseStatus            `json:"status"`
	PreviousServiceRevisionID  uint64                       `json:"previousServiceRevisionId,omitempty"`
	CandidateServiceRevisionID uint64                       `json:"candidateServiceRevisionId,omitempty"`
	RollbackServiceRevisionID  uint64                       `json:"rollbackServiceRevisionId,omitempty"`
	RollbackRequired           bool                         `json:"rollbackRequired,omitempty"`
	ErrorCode                  string                       `json:"errorCode,omitempty"`
	ErrorMessage               string                       `json:"errorMessage,omitempty"`
	RequestedBy                string                       `json:"requestedBy"`
	CreatedAt                  string                       `json:"createdAt"`
	UpdatedAt                  string                       `json:"updatedAt"`
}

type CreateTestReleaseRequest struct {
	BindingID string                       `json:"bindingId"`
	Receipt   artifactrepositoryv1.Receipt `json:"receipt"`
}

type ServiceIntentRequest struct {
	Intent backendcompositionv1.ApplicationIntent `json:"intent"`
}

// Service is the narrow BFF port consumed by HTTP handlers. Implementations
// may reach local or cluster capabilities. Target is resolved from the active
// Portal management binding by the BFF and cannot be supplied as routing fields
// by a browser.
