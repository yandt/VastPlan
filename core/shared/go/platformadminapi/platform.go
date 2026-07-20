// Package platformadminapi defines the browser-facing platform administration
// contract. It intentionally contains domain DTOs only: transport, plugin IDs,
// NATS subjects and repository credentials stay behind the Portal Edge.
package platformadminapi

import (
	"context"
	"encoding/json"
	"errors"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const (
	SettingsCapability    = "platform.settings"
	CredentialsCapability = "platform.credentials"
	DatabaseCapability    = "platform.database"
	ArtifactsCapability   = "platform.artifacts.repository"
	DeploymentCapability  = "platform.deployment"
)

var (
	ErrInvalid  = errors.New("平台管理请求无效")
	ErrNotFound = errors.New("平台管理资源不存在")
	ErrConflict = errors.New("平台管理资源版本冲突")
)

type Setting struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Version   int64           `json:"version"`
	UpdatedAt string          `json:"updatedAt"`
}

type PutSettingRequest struct {
	Value     json.RawMessage `json:"value"`
	IfVersion *int64          `json:"ifVersion,omitempty"`
}

type CredentialMetadata struct {
	Name       string `json:"name"`
	Version    int64  `json:"version"`
	KeyVersion string `json:"keyVersion"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
	Revoked    bool   `json:"revoked"`
}

// PutCredentialRequest is deliberately write-only. No response type and no
// read operation in this package can carry plaintext or ciphertext.
type PutCredentialRequest struct {
	Value string `json:"value"`
}

type DatabaseConnection struct {
	Name       string                   `json:"name"`
	ResourceID string                   `json:"resourceId"`
	Revision   uint64                   `json:"revision"`
	ProviderID string                   `json:"providerId"`
	Endpoint   string                   `json:"endpoint"`
	Database   string                   `json:"database,omitempty"`
	Options    json.RawMessage          `json:"options"`
	Pool       databasev1.PoolPolicy    `json:"pool"`
	Runtime    string                   `json:"runtime"`
	Credential DatabaseCredentialStatus `json:"credential"`
}

type DatabaseCredentialStatus struct {
	Managed bool  `json:"managed"`
	Version int64 `json:"version"`
}

// PutDatabaseConnectionRequest accepts credential material only as a
// write-only input to the database plugin. The value is omitted on ordinary
// edits to retain the currently managed credential and is never returned.
type PutDatabaseConnectionRequest struct {
	ProviderID      string                 `json:"providerId"`
	Endpoint        string                 `json:"endpoint"`
	Database        string                 `json:"database,omitempty"`
	Options         json.RawMessage        `json:"options"`
	Pool            *databasev1.PoolPolicy `json:"pool,omitempty"`
	CredentialValue string                 `json:"credentialValue,omitempty"`
}

type DatabaseProbe struct {
	Ready   bool   `json:"ready"`
	Message string `json:"message,omitempty"`
}

type ArtifactRepositoryStatus struct {
	Ready           bool   `json:"ready"`
	Listen          string `json:"listen,omitempty"`
	StorageProvider string `json:"storageProvider,omitempty"`
}

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
	ID            uint64                                      `json:"id"`
	Deployment    string                                      `json:"deployment"`
	Status        ServiceRevisionStatus                       `json:"status"`
	Active        bool                                        `json:"active"`
	Composition   backendcompositionv1.ApplicationComposition `json:"composition"`
	Preview       deploymentv2.Deployment                     `json:"preview"`
	PreviewDigest string                                      `json:"previewDigest"`
	KVRevision    uint64                                      `json:"kvRevision,omitempty"`
	SubmittedBy   string                                      `json:"submittedBy,omitempty"`
	ApprovedBy    string                                      `json:"approvedBy,omitempty"`
	PublishedBy   string                                      `json:"publishedBy,omitempty"`
	CreatedAt     string                                      `json:"createdAt"`
	UpdatedAt     string                                      `json:"updatedAt"`
}

type ServiceAuditEvent struct {
	ID         uint64 `json:"id"`
	RevisionID uint64 `json:"revisionId"`
	Deployment string `json:"deployment"`
	Action     string `json:"action"`
	ActorID    string `json:"actorId"`
	At         string `json:"at"`
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
	ID                         uint64               `json:"id"`
	BindingID                  string               `json:"bindingId"`
	Artifact                   pluginv1.ArtifactRef `json:"artifact"`
	SHA256                     string               `json:"sha256"`
	RepositoryRevision         uint64               `json:"repositoryRevision"`
	Status                     TestReleaseStatus    `json:"status"`
	PreviousServiceRevisionID  uint64               `json:"previousServiceRevisionId,omitempty"`
	CandidateServiceRevisionID uint64               `json:"candidateServiceRevisionId,omitempty"`
	RollbackServiceRevisionID  uint64               `json:"rollbackServiceRevisionId,omitempty"`
	RollbackRequired           bool                 `json:"rollbackRequired,omitempty"`
	ErrorCode                  string               `json:"errorCode,omitempty"`
	ErrorMessage               string               `json:"errorMessage,omitempty"`
	RequestedBy                string               `json:"requestedBy"`
	CreatedAt                  string               `json:"createdAt"`
	UpdatedAt                  string               `json:"updatedAt"`
}

type CreateTestReleaseRequest struct {
	BindingID          string               `json:"bindingId"`
	Artifact           pluginv1.ArtifactRef `json:"artifact"`
	SHA256             string               `json:"sha256"`
	RepositoryRevision uint64               `json:"repositoryRevision"`
}

type ServiceCompositionRequest struct {
	Composition backendcompositionv1.ApplicationComposition `json:"composition"`
}

// Service is the narrow BFF port consumed by HTTP handlers. Implementations
// may reach local or cluster capabilities. Target is resolved from the active
// Portal management binding by Edge and cannot be supplied as routing fields by
// a browser.
type Service interface {
	ListSettings(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) ([]Setting, error)
	PutSetting(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, PutSettingRequest) (Setting, error)
	DeleteSetting(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, *int64) error
	ListCredentials(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) ([]CredentialMetadata, error)
	PutCredential(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, PutCredentialRequest) (CredentialMetadata, error)
	RotateCredential(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (CredentialMetadata, error)
	RevokeCredential(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (CredentialMetadata, error)
	ListDatabaseConnections(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]DatabaseConnection, error)
	PutDatabaseConnection(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, PutDatabaseConnectionRequest) (DatabaseConnection, error)
	DeleteDatabaseConnection(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) error
	ProbeDatabaseConnection(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (DatabaseProbe, error)
	ArtifactRepositoryStatus(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactRepositoryStatus, error)
	ListManagedNodes(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]ManagedNode, error)
	PutManagedNode(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, PutManagedNodeRequest) (ManagedNode, error)
	ListBootstrapJobs(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]BootstrapJob, error)
	CreateBootstrapJob(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (BootstrapJob, error)
	ApproveBootstrapJob(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (BootstrapJob, error)
	ListDeploymentTargets(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]DeploymentTarget, error)
	ListServiceRevisions(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]ServiceRevision, error)
	CreateServiceDraft(context.Context, portalapi.Principal, portalapi.ManagementTarget, ServiceCompositionRequest) (ServiceRevision, error)
	UpdateServiceDraft(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64, ServiceCompositionRequest) (ServiceRevision, error)
	SubmitServiceDraft(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (ServiceRevision, error)
	ApproveServiceRevision(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (ServiceRevision, error)
	PublishServiceRevision(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (ServiceRevision, error)
	RollbackServiceRevision(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (ServiceRevision, error)
	ListServiceRevisionAudit(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) ([]ServiceAuditEvent, error)
	ListTestTargetBindings(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]TestTargetBinding, error)
	PutTestTargetBinding(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, PutTestTargetBindingRequest) (TestTargetBinding, error)
	ListTestReleases(context.Context, portalapi.Principal, portalapi.ManagementTarget) ([]TestRelease, error)
	CreateTestRelease(context.Context, portalapi.Principal, portalapi.ManagementTarget, CreateTestReleaseRequest) (TestRelease, error)
	RollbackTestRelease(context.Context, portalapi.Principal, portalapi.ManagementTarget, uint64) (TestRelease, error)
}
