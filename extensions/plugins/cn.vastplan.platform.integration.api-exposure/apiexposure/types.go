// Package apiexposure governs stable public API bindings and ephemeral data-plane access.
package apiexposure

import (
	"time"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
)

const (
	PluginID      = "cn.vastplan.platform.integration.api-exposure"
	PluginVersion = "0.3.0"
	Capability    = "platform.api-exposure"
)

type Status string

const (
	StatusDraft           Status = "Draft"
	StatusPendingApproval Status = "PendingApproval"
	StatusApproved        Status = "Approved"
	StatusPublished       Status = "Published"
	StatusSuperseded      Status = "Superseded"
	StatusRetired         Status = "Retired"
)

type Principal struct {
	ID       string
	TenantID string
	Roles    []string
}

type ContractSelector struct {
	PluginID       string `json:"pluginId"`
	ArtifactSHA256 string `json:"artifactSha256"`
	ContributionID string `json:"contributionId"`
}

type ExposureInput struct {
	DisplayName         string                     `json:"displayName"`
	PortalID            string                     `json:"portalId,omitempty"`
	Hosts               []string                   `json:"hosts"`
	Authentication      apiv1.AuthenticationPolicy `json:"authentication"`
	RequiredPermissions []string                   `json:"requiredPermissions"`
	Limits              apiv1.ExposureLimits       `json:"limits"`
	Target              apiv1.ExposureTarget       `json:"target"`
}

type CreateDraftRequest struct {
	BaseExposureID string           `json:"baseExposureId,omitempty"`
	Contract       ContractSelector `json:"contract"`
	Input          ExposureInput    `json:"input"`
}

type UpdateDraftRequest struct {
	RevisionID       uint64           `json:"revisionId"`
	ExpectedRevision uint64           `json:"expectedRevision"`
	Contract         ContractSelector `json:"contract"`
	Input            ExposureInput    `json:"input"`
}

type Revision struct {
	ID          uint64                     `json:"id"`
	Status      Status                     `json:"status"`
	Exposure    apiv1.Exposure             `json:"exposure"`
	Contract    apiv1.ContractContribution `json:"contract"`
	SubmittedBy string                     `json:"submittedBy,omitempty"`
	ApprovedBy  string                     `json:"approvedBy,omitempty"`
	PublishedBy string                     `json:"publishedBy,omitempty"`
	CreatedAt   time.Time                  `json:"createdAt"`
	UpdatedAt   time.Time                  `json:"updatedAt"`
}

type DataPlaneInput struct {
	Hosts                  []string                        `json:"hosts"`
	Service                apiv1.DataPlaneServiceReference `json:"service"`
	AllowedModes           []string                        `json:"allowedModes"`
	AllowedEndpointOrigins []string                        `json:"allowedEndpointOrigins"`
	TLSIdentityPrefix      string                          `json:"tlsIdentityPrefix"`
	Authentication         apiv1.AuthenticationPolicy      `json:"authentication"`
	RequiredPermissions    []string                        `json:"requiredPermissions"`
	MaxObjectBytes         uint64                          `json:"maxObjectBytes"`
}

type CreateDataPlaneDraftRequest struct {
	BaseExposureID string         `json:"baseExposureId,omitempty"`
	Input          DataPlaneInput `json:"input"`
}

type DataPlaneRevision struct {
	ID          uint64                  `json:"id"`
	Status      Status                  `json:"status"`
	Exposure    apiv1.DataPlaneExposure `json:"exposure"`
	SubmittedBy string                  `json:"submittedBy,omitempty"`
	ApprovedBy  string                  `json:"approvedBy,omitempty"`
	PublishedBy string                  `json:"publishedBy,omitempty"`
	CreatedAt   time.Time               `json:"createdAt"`
	UpdatedAt   time.Time               `json:"updatedAt"`
}

type AuditEvent struct {
	ID         uint64    `json:"id"`
	TenantID   string    `json:"tenantId"`
	ResourceID string    `json:"resourceId"`
	RevisionID uint64    `json:"revisionId"`
	Action     string    `json:"action"`
	Actor      string    `json:"actor"`
	At         time.Time `json:"at"`
}

type EndpointLeaseRequest = apiv1.EndpointLeaseRegistration
type EndpointLeaseRenewal = apiv1.EndpointLeaseRenewal
type EndpointLeaseRevocation = apiv1.EndpointLeaseRevocation

type TicketConsumption struct {
	Ticket     string `json:"ticket"`
	InstanceID string `json:"instanceId"`
}

type RuntimeCaller struct {
	PluginID string
	TenantID string
}

type TicketRequest struct {
	DataPlaneExposureID string `json:"dataPlaneExposureId"`
	Method              string `json:"method"`
	Resource            string `json:"resource"`
	ContentSHA256       string `json:"contentSha256,omitempty"`
}

type TicketGrant struct {
	Endpoint  string    `json:"endpoint"`
	LeaseID   string    `json:"leaseId"`
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type TicketClaims = apiv1.DataPlaneTicketClaims

type TicketInstallation struct {
	Target apiv1.CapabilityTarget `json:"-"`
	Ticket string                 `json:"ticket"`
	Claims TicketClaims           `json:"claims"`
}
