package portalapi

import (
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

const (
	WorkflowPublicationFeatureID       = "platform.portal.publication"
	WorkflowPublicationResourceKind    = "portal.publication"
	WorkflowPublicationReleaseActionID = "portal.release"
	PreparePortalPublicationOperation  = "preparePortalPublication"
	ExecutePublicationReleaseOperation = "executePublicationRelease"
)

// PortalConfiguration is the complete editable input of a Portal WorkingCopy
// and the frozen inline source of a Publication when version control is off.
type PortalConfiguration struct {
	Platform    frontendcompositionv1.PlatformProfile        `json:"platform"`
	Application frontendcompositionv1.ApplicationComposition `json:"application"`
	Services    []frontendcompositionv1.ManagedService       `json:"services"`
}

type PortalWorkingCopy struct {
	TenantID      string              `json:"tenantId"`
	PortalID      string              `json:"portalId"`
	Revision      uint64              `json:"revision"`
	Configuration PortalConfiguration `json:"configuration"`
	Digest        string              `json:"digest"`
	UpdatedBy     string              `json:"updatedBy,omitempty"`
	CreatedAt     string              `json:"createdAt"`
	UpdatedAt     string              `json:"updatedAt"`
}

type PortalPublicationSourceKind string

const (
	PortalPublicationSourceInline    PortalPublicationSourceKind = "inline"
	PortalPublicationSourceWorkspace PortalPublicationSourceKind = "workspace"
)

type PortalPublicationSource struct {
	Kind              PortalPublicationSourceKind `json:"kind"`
	Configuration     *PortalConfiguration        `json:"configuration,omitempty"`
	EnvironmentID     string                      `json:"environmentId,omitempty"`
	EnvironmentDigest string                      `json:"environmentDigest,omitempty"`
	VersionRef        *versioningv1.VersionRef    `json:"versionRef,omitempty"`
}

type PortalPublication struct {
	ID              uint64                  `json:"id"`
	TenantID        string                  `json:"tenantId"`
	PortalID        string                  `json:"portalId"`
	WorkingRevision uint64                  `json:"workingRevision"`
	Status          Status                  `json:"status"`
	Digest          string                  `json:"digest"`
	Source          PortalPublicationSource `json:"source"`
	Resolved        PortalSpec              `json:"resolved"`
	SubmittedBy     string                  `json:"submittedBy"`
	ApprovedBy      string                  `json:"approvedBy,omitempty"`
	PublishedBy     string                  `json:"publishedBy,omitempty"`
	CreatedAt       string                  `json:"createdAt"`
	UpdatedAt       string                  `json:"updatedAt"`
	Approval        *approvalv2.Decision    `json:"approval,omitempty"`
}

type PortalApprovalRequest struct {
	Review approvalv2.ReviewEvidence `json:"review"`
}

type PortalVersionControlAvailability string

const (
	PortalVersionControlDisabled    PortalVersionControlAvailability = "disabled"
	PortalVersionControlAvailable   PortalVersionControlAvailability = "available"
	PortalVersionControlUnavailable PortalVersionControlAvailability = "unavailable"
)

type PortalVersionControlStatus struct {
	Enabled      bool                             `json:"enabled"`
	Availability PortalVersionControlAvailability `json:"availability"`
	Capabilities []string                         `json:"capabilities"`
}

// PortalVersionHistoryEntry is the Portal aggregate's confirmed projection of
// one Workspace commit. Ledger records that are not confirmed by the aggregate
// are intentionally invisible here.
type PortalVersionHistoryEntry struct {
	PublicationID       uint64                  `json:"publicationId"`
	EnvironmentID       string                  `json:"environmentId"`
	EnvironmentDigest   string                  `json:"environmentDigest"`
	VersionRef          versioningv1.VersionRef `json:"versionRef"`
	ConfigurationDigest string                  `json:"configurationDigest"`
	ActorID             string                  `json:"actorId"`
	CreatedAt           string                  `json:"createdAt"`
}

type PortalVersionHistory struct {
	PortalID string                      `json:"portalId"`
	Entries  []PortalVersionHistoryEntry `json:"entries"`
}

type PortalVersionSnapshot struct {
	Entry         PortalVersionHistoryEntry `json:"entry"`
	Configuration PortalConfiguration       `json:"configuration"`
}

type PortalVersionComparison struct {
	Left          PortalVersionHistoryEntry       `json:"left"`
	Right         PortalVersionHistoryEntry       `json:"right"`
	Dirty         bool                            `json:"dirty"`
	DiffAvailable bool                            `json:"diffAvailable"`
	ChangedPaths  []string                        `json:"changedPaths,omitempty"`
	Summary       versionresourcev1.ChangeSummary `json:"summary"`
}

// PortalVersion is an internal transition record used by test-release and
// activation workflows. It is not part of the Portal governance wire model.
type PortalVersion struct {
	ID            uint64              `json:"id"`
	Number        uint64              `json:"number"`
	TenantID      string              `json:"tenantId"`
	PortalID      string              `json:"portalId"`
	Status        Status              `json:"status"`
	Configuration PortalConfiguration `json:"configuration"`
	Resolved      PortalSpec          `json:"resolved"`
	SubmittedBy   string              `json:"submittedBy,omitempty"`
	ApprovedBy    string              `json:"approvedBy,omitempty"`
	PublishedBy   string              `json:"publishedBy,omitempty"`
	CreatedAt     string              `json:"createdAt"`
	UpdatedAt     string              `json:"updatedAt"`
}

type PortalReleaseStatus = ActivationStatus
type PortalReleasePhase = ActivationPhase

// PortalRelease records that one exact, published Publication became live.
// Rollback creates another release and never mutates earlier release facts.
type PortalRelease struct {
	ID                 uint64                       `json:"id"`
	TenantID           string                       `json:"tenantId"`
	PortalID           string                       `json:"portalId"`
	PublicationID      uint64                       `json:"publicationId"`
	Status             PortalReleaseStatus          `json:"status"`
	PreviousReleaseID  uint64                       `json:"previousReleaseId,omitempty"`
	Resolved           PortalSpec                   `json:"resolved"`
	ArtifactReferences []pluginv1.ArtifactReference `json:"artifactReferences,omitempty"`
	ReferencePending   bool                         `json:"referencePending,omitempty"`
	Phases             []PortalReleasePhase         `json:"phases"`
	ActorID            string                       `json:"actorId"`
	Reason             string                       `json:"reason,omitempty"`
	CreatedAt          string                       `json:"createdAt"`
}

// Portal is the only online governance aggregate exposed to administrators.
type Portal struct {
	ID                   string                     `json:"id"`
	TenantID             string                     `json:"tenantId"`
	WorkingCopy          *PortalWorkingCopy         `json:"workingCopy,omitempty"`
	PendingPublication   *PortalPublication         `json:"pendingPublication,omitempty"`
	PublishedPublication *PortalPublication         `json:"publishedPublication,omitempty"`
	VersionControl       PortalVersionControlStatus `json:"versionControl"`
	Releases             []PortalRelease            `json:"releases"`
	CurrentReleaseID     uint64                     `json:"currentReleaseId,omitempty"`
	CreatedAt            string                     `json:"createdAt"`
	UpdatedAt            string                     `json:"updatedAt"`
}

type CreatePortalRequest struct {
	PortalID      string              `json:"portalId"`
	Configuration PortalConfiguration `json:"configuration"`
}

type PortalReleaseRequest struct {
	PortalVersionID          uint64 `json:"portalVersionId"`
	ExpectedCurrentReleaseID uint64 `json:"expectedCurrentReleaseId"`
	Reason                   string `json:"reason,omitempty"`
}

type SavePortalWorkingCopyRequest struct {
	ExpectedRevision uint64              `json:"expectedRevision"`
	Configuration    PortalConfiguration `json:"configuration"`
}

type SubmitPortalPublicationRequest struct {
	ExpectedWorkingRevision uint64 `json:"expectedWorkingRevision"`
}

type PortalPublicationReleaseRequest struct {
	PublicationID            uint64 `json:"publicationId"`
	ExpectedCurrentReleaseID uint64 `json:"expectedCurrentReleaseId"`
	Reason                   string `json:"reason,omitempty"`
}

type RestorePortalVersionRequest struct {
	VersionID               string `json:"versionId"`
	ExpectedWorkingRevision uint64 `json:"expectedWorkingRevision"`
}

type PortalGovernanceSnapshot struct {
	Portals          []Portal             `json:"portals"`
	CreationTemplate *PortalConfiguration `json:"creationTemplate,omitempty"`
}
