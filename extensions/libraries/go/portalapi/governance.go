package portalapi

import (
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

// PortalConfiguration is the complete editable input of one Portal version.
// Platform settings and service bindings are version-owned configuration, not
// independently governed resources.
type PortalConfiguration struct {
	Platform    frontendcompositionv1.PlatformProfile        `json:"platform"`
	Application frontendcompositionv1.ApplicationComposition `json:"application"`
	Services    []frontendcompositionv1.ManagedService       `json:"services"`
}

// PortalVersion is an immutable version after publication. ID is the opaque
// storage identity; Number is monotonic only inside one Portal lineage.
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

// PortalRelease records that one exact, published PortalVersion became live.
// Rollback creates another release and never mutates version history.
type PortalRelease struct {
	ID                 uint64                       `json:"id"`
	TenantID           string                       `json:"tenantId"`
	PortalID           string                       `json:"portalId"`
	PortalVersionID    uint64                       `json:"portalVersionId"`
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
	ID               string          `json:"id"`
	TenantID         string          `json:"tenantId"`
	Versions         []PortalVersion `json:"versions"`
	Releases         []PortalRelease `json:"releases"`
	CurrentReleaseID uint64          `json:"currentReleaseId,omitempty"`
	CreatedAt        string          `json:"createdAt"`
	UpdatedAt        string          `json:"updatedAt"`
}

type PortalVersionRequest struct {
	PortalID      string              `json:"portalId"`
	Configuration PortalConfiguration `json:"configuration"`
}

type PortalReleaseRequest struct {
	PortalVersionID          uint64 `json:"portalVersionId"`
	ExpectedCurrentReleaseID uint64 `json:"expectedCurrentReleaseId"`
	Reason                   string `json:"reason,omitempty"`
}

type PortalGovernanceSnapshot struct {
	Portals []Portal `json:"portals"`
}
