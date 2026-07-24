// Package platformadminapi defines the browser-facing platform administration
// contract. It intentionally contains domain DTOs only: transport, plugin IDs,
// NATS subjects and repository credentials stay behind the trusted Portal host.
package platformadminapi

import (
	"context"
	"encoding/json"
	"errors"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const (
	SettingsCapability            = "platform.settings"
	CredentialsCapability         = "platform.credentials"
	DatabaseCapability            = "platform.database"
	ArtifactsCapability           = "platform.artifacts.repository"
	DeploymentCapability          = "platform.deployment"
	PluginConfigurationCapability = "platform.plugin-configuration"
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
	Ready           bool                        `json:"ready"`
	Listen          string                      `json:"listen,omitempty"`
	StorageProvider string                      `json:"storageProvider,omitempty"`
	StorageVolumeID string                      `json:"storageVolumeId,omitempty"`
	Catalog         ArtifactCatalogStatus       `json:"catalog"`
	Migration       ArtifactRepositoryMigration `json:"migration"`
}

type ArtifactCatalogStatus struct {
	Revision                   uint64 `json:"revision"`
	Artifacts                  int    `json:"artifacts"`
	InventorySHA256            string `json:"inventorySHA256,omitempty"`
	PublicationRevision        uint64 `json:"publicationRevision"`
	PublicationInventorySHA256 string `json:"publicationInventorySHA256,omitempty"`
}

type ArtifactCatalogQuery struct {
	PluginID     string `json:"pluginId,omitempty"`
	PluginPrefix string `json:"pluginPrefix,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	Publisher    string `json:"publisher,omitempty"`
	Version      string `json:"version,omitempty"`
	Channel      string `json:"channel,omitempty"`
	Target       string `json:"target,omitempty"`
	Lifecycle    string `json:"lifecycle,omitempty"`
	Page         int    `json:"page"`
	PageSize     int    `json:"pageSize"`
}

type ArtifactCatalogEntry struct {
	Ref                pluginv1.ArtifactRef                  `json:"ref"`
	SHA256             string                                `json:"sha256"`
	Size               int64                                 `json:"size"`
	Publisher          string                                `json:"publisher"`
	KeyID              string                                `json:"keyId"`
	SignedAt           string                                `json:"signedAt"`
	PublishedAt        string                                `json:"publishedAt"`
	RepositoryRevision uint64                                `json:"repositoryRevision"`
	Name               string                                `json:"name"`
	Description        string                                `json:"description"`
	Namespace          string                                `json:"namespace"`
	License            string                                `json:"license,omitempty"`
	Targets            []string                              `json:"targets"`
	Platforms          []string                              `json:"platforms,omitempty"`
	LifecycleStatus    string                                `json:"lifecycleStatus"`
	LifecycleRevision  uint64                                `json:"lifecycleRevision,omitempty"`
	LifecycleReason    string                                `json:"lifecycleReason,omitempty"`
	Replacement        *pluginv1.ArtifactRequirement         `json:"replacement,omitempty"`
	SBOM               *ArtifactSBOMDeclaration              `json:"sbom,omitempty"`
	PythonLock         *ArtifactPythonLockDeclaration        `json:"pythonLock,omitempty"`
	Provenance         *ArtifactProvenanceDeclaration        `json:"provenance,omitempty"`
	SecurityAdmission  *ArtifactSecurityAdmissionDeclaration `json:"securityAdmission,omitempty"`
	SecurityStatus     *ArtifactSecurityStatusEvidence       `json:"securityStatus,omitempty"`
}

type ArtifactSBOMDeclaration struct {
	Format      string `json:"format"`
	SpecVersion string `json:"specVersion"`
	SHA256      string `json:"sha256"`
}

type ArtifactSBOMEvidence struct {
	ArtifactSBOMDeclaration
	SerialNumber string `json:"serialNumber,omitempty"`
	Components   int    `json:"components"`
	Verification string `json:"verification"`
}

type ArtifactPythonLockDeclaration struct {
	Format      string `json:"format"`
	SpecVersion string `json:"specVersion"`
	SHA256      string `json:"sha256"`
}

type ArtifactPythonLockEvidence struct {
	ArtifactPythonLockDeclaration
	RequiresPython string `json:"requiresPython"`
	CreatedBy      string `json:"createdBy"`
	Packages       int    `json:"packages"`
	Wheels         int    `json:"wheels"`
	Verification   string `json:"verification"`
}

type ArtifactProvenanceDeclaration struct {
	ProvenanceSHA256   string `json:"provenanceSha256"`
	VerificationSHA256 string `json:"verificationSha256"`
	PredicateType      string `json:"predicateType"`
	BuilderID          string `json:"builderId"`
	BuildType          string `json:"buildType"`
	ProviderID         string `json:"providerId"`
	KeyID              string `json:"keyId"`
	PolicyID           string `json:"policyId"`
	VerifiedAt         string `json:"verifiedAt"`
	ExpiresAt          string `json:"expiresAt"`
}

type ArtifactProvenanceEvidence struct {
	ArtifactProvenanceDeclaration
	Sources      int    `json:"sources"`
	Verification string `json:"verification"`
}

type ArtifactSecurityAdmissionDeclaration struct {
	AdmissionSHA256      string `json:"admissionSha256"`
	ProviderID           string `json:"providerId"`
	KeyID                string `json:"keyId"`
	PolicyID             string `json:"policyId"`
	ScannerID            string `json:"scannerId"`
	ScannerVersion       string `json:"scannerVersion"`
	DatabaseRevision     string `json:"databaseRevision"`
	Decision             string `json:"decision"`
	EvaluatedAt          string `json:"evaluatedAt"`
	ExpiresAt            string `json:"expiresAt"`
	Critical             uint64 `json:"critical"`
	High                 uint64 `json:"high"`
	Medium               uint64 `json:"medium"`
	Low                  uint64 `json:"low"`
	UnknownVulnerability uint64 `json:"unknownVulnerability"`
	DeniedLicense        uint64 `json:"deniedLicense"`
	UnknownLicense       uint64 `json:"unknownLicense"`
}

type ArtifactSecurityAdmissionEvidence struct {
	ArtifactSecurityAdmissionDeclaration
	VulnerabilityReportSHA256 string `json:"vulnerabilityReportSha256,omitempty"`
	LicenseReportSHA256       string `json:"licenseReportSha256,omitempty"`
	Verification              string `json:"verification"`
}

type ArtifactSecurityStatusEvidence struct {
	Sequence                  uint64 `json:"sequence"`
	RecordSHA256              string `json:"recordSha256"`
	PreviousSHA256            string `json:"previousSha256"`
	Decision                  string `json:"decision"`
	DatabaseRevision          string `json:"databaseRevision"`
	EvaluatedAt               string `json:"evaluatedAt"`
	ExpiresAt                 string `json:"expiresAt"`
	Critical                  uint64 `json:"critical"`
	High                      uint64 `json:"high"`
	DeniedLicense             uint64 `json:"deniedLicense"`
	UnknownLicense            uint64 `json:"unknownLicense"`
	VulnerabilityReportSHA256 string `json:"vulnerabilityReportSha256,omitempty"`
	LicenseReportSHA256       string `json:"licenseReportSha256,omitempty"`
	Verification              string `json:"verification"`
}

type ArtifactCatalogPage struct {
	Revision uint64                 `json:"revision"`
	Total    int                    `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"pageSize"`
	Items    []ArtifactCatalogEntry `json:"items"`
}

type ArtifactLifecycleRequest struct {
	Ref              pluginv1.ArtifactRef          `json:"ref"`
	Status           string                        `json:"status"`
	Reason           string                        `json:"reason"`
	Replacement      *pluginv1.ArtifactRequirement `json:"replacement,omitempty"`
	ExpectedRevision uint64                        `json:"expectedRevision"`
}

type ArtifactLifecycleResult struct {
	Revision uint64 `json:"revision"`
	Entry    struct {
		Ref               pluginv1.ArtifactRef          `json:"ref"`
		LifecycleStatus   string                        `json:"lifecycleStatus"`
		LifecycleRevision uint64                        `json:"lifecycleRevision"`
		LifecycleReason   string                        `json:"lifecycleReason,omitempty"`
		Replacement       *pluginv1.ArtifactRequirement `json:"replacement,omitempty"`
	} `json:"entry"`
}

type ArtifactPublication struct {
	ID                                    string               `json:"id"`
	Revision                              uint64               `json:"revision"`
	Status                                string               `json:"status"`
	Source                                pluginv1.ArtifactRef `json:"source"`
	Target                                pluginv1.ArtifactRef `json:"target"`
	SHA256                                string               `json:"sha256"`
	Publisher                             string               `json:"publisher"`
	KeyID                                 string               `json:"keyId"`
	SourceAttestationSHA256               string               `json:"sourceAttestationSha256"`
	SourceProvenanceSHA256                string               `json:"sourceProvenanceSha256,omitempty"`
	SourceProvenanceVerificationSHA256    string               `json:"sourceProvenanceVerificationSha256,omitempty"`
	SourceSecurityAdmissionSHA256         string               `json:"sourceSecurityAdmissionSha256,omitempty"`
	PublishedAttestationSHA256            string               `json:"publishedAttestationSha256,omitempty"`
	PublishedProvenanceSHA256             string               `json:"publishedProvenanceSha256,omitempty"`
	PublishedProvenanceVerificationSHA256 string               `json:"publishedProvenanceVerificationSha256,omitempty"`
	PublishedSecurityAdmissionSHA256      string               `json:"publishedSecurityAdmissionSha256,omitempty"`
	Reason                                string               `json:"reason"`
	SubmittedBy                           string               `json:"submittedBy"`
	ApprovedBy                            string               `json:"approvedBy,omitempty"`
	SubmittedAt                           string               `json:"submittedAt"`
	ExpiresAt                             string               `json:"expiresAt"`
	ApprovedAt                            string               `json:"approvedAt,omitempty"`
	PublishedAt                           string               `json:"publishedAt,omitempty"`
	TerminalReason                        string               `json:"terminalReason,omitempty"`
	TerminalBy                            string               `json:"terminalBy,omitempty"`
	TerminalAt                            string               `json:"terminalAt,omitempty"`
}

type ArtifactPublicationRequest struct {
	Source           pluginv1.ArtifactRef `json:"source"`
	TargetChannel    string               `json:"targetChannel"`
	Reason           string               `json:"reason"`
	ExpectedRevision uint64               `json:"expectedRevision"`
}
type ArtifactPublicationApprovalRequest struct {
	ID               string `json:"id"`
	ExpectedRevision uint64 `json:"expectedRevision"`
}
type ArtifactPublicationTransitionRequest struct {
	ID               string `json:"id"`
	ExpectedRevision uint64 `json:"expectedRevision"`
	Reason           string `json:"reason"`
}
type ArtifactPublicationPage struct {
	Revision uint64                `json:"revision"`
	Items    []ArtifactPublication `json:"items"`
}
type ArtifactSupplyChainEvidence struct {
	Ref                pluginv1.ArtifactRef               `json:"ref"`
	SHA256             string                             `json:"sha256"`
	Size               int64                              `json:"size"`
	Publisher          string                             `json:"publisher"`
	KeyID              string                             `json:"keyId"`
	SignedAt           string                             `json:"signedAt"`
	AttestationSHA256  string                             `json:"attestationSha256"`
	Verification       string                             `json:"verification"`
	Name               string                             `json:"name"`
	Description        string                             `json:"description"`
	License            string                             `json:"license,omitempty"`
	Targets            []string                           `json:"targets"`
	Engines            map[string]string                  `json:"engines"`
	RepositoryRevision uint64                             `json:"repositoryRevision"`
	LifecycleStatus    string                             `json:"lifecycleStatus"`
	Publications       []ArtifactPublication              `json:"publications"`
	SBOM               *ArtifactSBOMEvidence              `json:"sbom,omitempty"`
	PythonLock         *ArtifactPythonLockEvidence        `json:"pythonLock,omitempty"`
	Provenance         *ArtifactProvenanceEvidence        `json:"provenance,omitempty"`
	SecurityAdmission  *ArtifactSecurityAdmissionEvidence `json:"securityAdmission,omitempty"`
	SecurityStatus     *ArtifactSecurityStatusEvidence    `json:"securityStatus,omitempty"`
}

type ArtifactReferenceSnapshot struct {
	TenantID    string                             `json:"tenantId"`
	PublisherID string                             `json:"publisherId"`
	Value       pluginv1.ArtifactReferenceSnapshot `json:"value"`
	ReportedAt  string                             `json:"reportedAt"`
	ExpiresAt   string                             `json:"expiresAt,omitempty"`
}

type ArtifactReferencePage struct {
	Revision uint64                      `json:"revision"`
	Items    []ArtifactReferenceSnapshot `json:"items"`
}

type ArtifactGCBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ArtifactGCCandidate struct {
	Ref       pluginv1.ArtifactRef `json:"ref"`
	SHA256    string               `json:"sha256"`
	Size      int64                `json:"size"`
	Lifecycle string               `json:"lifecycle"`
}

type ArtifactGCPlan struct {
	SchemaVersion     string                `json:"schemaVersion"`
	PlanID            string                `json:"planId,omitempty"`
	Ready             bool                  `json:"ready"`
	CreatedAt         string                `json:"createdAt"`
	CatalogRevision   uint64                `json:"catalogRevision"`
	ReferenceRevision uint64                `json:"referenceRevision"`
	Candidates        []ArtifactGCCandidate `json:"candidates"`
	Bytes             int64                 `json:"bytes"`
	Blockers          []ArtifactGCBlocker   `json:"blockers,omitempty"`
}

type ArtifactGCRecord struct {
	RetirementID  string               `json:"retirementId"`
	Ref           pluginv1.ArtifactRef `json:"ref"`
	SHA256        string               `json:"sha256"`
	Size          int64                `json:"size"`
	Lifecycle     string               `json:"lifecycle"`
	Status        string               `json:"status"`
	QuarantinedAt string               `json:"quarantinedAt"`
	SweepAfter    string               `json:"sweepAfter"`
	SweptAt       string               `json:"sweptAt,omitempty"`
}

type ArtifactGCStatus struct {
	Revision uint64             `json:"revision"`
	Items    []ArtifactGCRecord `json:"items"`
}

type QuarantineArtifactsRequest struct {
	PlanID     string `json:"planId"`
	GraceHours int64  `json:"graceHours"`
}

type ArtifactCapacityBucket struct {
	Namespace string `json:"namespace"`
	Publisher string `json:"publisher"`
	Channel   string `json:"channel"`
	Artifacts int    `json:"artifacts"`
	Bytes     int64  `json:"bytes"`
}

type ArtifactQuotaUsage struct {
	ID           string `json:"id"`
	Namespace    string `json:"namespace,omitempty"`
	Publisher    string `json:"publisher,omitempty"`
	Channel      string `json:"channel,omitempty"`
	Artifacts    int    `json:"artifacts"`
	Bytes        int64  `json:"bytes"`
	MaxArtifacts int    `json:"maxArtifacts,omitempty"`
	MaxBytes     int64  `json:"maxBytes,omitempty"`
	Exceeded     bool   `json:"exceeded"`
}

type ArtifactCapacity struct {
	CatalogRevision      uint64                   `json:"catalogRevision"`
	GCRevision           uint64                   `json:"gcRevision"`
	ActiveArtifacts      int                      `json:"activeArtifacts"`
	ActiveBytes          int64                    `json:"activeBytes"`
	QuarantinedArtifacts int                      `json:"quarantinedArtifacts"`
	QuarantinedBytes     int64                    `json:"quarantinedBytes"`
	SweptArtifacts       int                      `json:"sweptArtifacts"`
	ReclaimedBytes       int64                    `json:"reclaimedBytes"`
	StoredBytes          int64                    `json:"storedBytes"`
	Buckets              []ArtifactCapacityBucket `json:"buckets"`
	Quotas               []ArtifactQuotaUsage     `json:"quotas"`
}

type ArtifactRepositoryMigration struct {
	MigrationID      string `json:"migrationId,omitempty"`
	Phase            string `json:"phase,omitempty"`
	SourceProvider   string `json:"sourceProvider,omitempty"`
	SourceVolumeID   string `json:"sourceVolumeId,omitempty"`
	TargetProvider   string `json:"targetProvider,omitempty"`
	TargetVolumeID   string `json:"targetVolumeId,omitempty"`
	Files            int64  `json:"files,omitempty"`
	Bytes            int64  `json:"bytes,omitempty"`
	Digest           string `json:"digest,omitempty"`
	ObservationUntil string `json:"observationUntil,omitempty"`
	LastError        string `json:"lastError,omitempty"`
	ConfiguredActive bool   `json:"configuredActive"`
	CanRollback      bool   `json:"canRollback"`
	CanFinalize      bool   `json:"canFinalize"`
	CanRelease       bool   `json:"canRelease"`
}

type PrepareArtifactMigrationRequest struct {
	MigrationID    string `json:"migrationId"`
	TargetProvider string `json:"targetProvider"`
	TargetVolumeID string `json:"targetVolumeId"`
}

type CutoverArtifactMigrationRequest struct {
	ObservationSeconds int64 `json:"observationSeconds"`
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
	ID                       uint64                                      `json:"id"`
	Deployment               string                                      `json:"deployment"`
	Status                   ServiceRevisionStatus                       `json:"status"`
	Active                   bool                                        `json:"active"`
	Composition              backendcompositionv1.ApplicationComposition `json:"composition"`
	Preview                  deploymentv2.Deployment                     `json:"preview"`
	PreviewDigest            string                                      `json:"previewDigest"`
	ArtifactReferences       []pluginv1.ArtifactReference                `json:"artifactReferences"`
	ConfigurationCatalog     pluginconfiguration.Catalog                 `json:"configurationCatalog"`
	ConfigurationCandidateID string                                      `json:"configurationCandidateId,omitempty"`
	ConfigurationID          string                                      `json:"configurationId,omitempty"`
	PreviousServiceRevision  uint64                                      `json:"previousServiceRevision,omitempty"`
	RollbackServiceRevision  uint64                                      `json:"rollbackServiceRevision,omitempty"`
	KVRevision               uint64                                      `json:"kvRevision,omitempty"`
	ReferencePending         bool                                        `json:"referencePending,omitempty"`
	SubmittedBy              string                                      `json:"submittedBy,omitempty"`
	ApprovedBy               string                                      `json:"approvedBy,omitempty"`
	PublishedBy              string                                      `json:"publishedBy,omitempty"`
	CreatedAt                string                                      `json:"createdAt"`
	UpdatedAt                string                                      `json:"updatedAt"`
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

type ServiceCompositionRequest struct {
	Composition backendcompositionv1.ApplicationComposition `json:"composition"`
}

// Service is the narrow BFF port consumed by HTTP handlers. Implementations
// may reach local or cluster capabilities. Target is resolved from the active
// Portal management binding by the BFF and cannot be supplied as routing fields
// by a browser.
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
	ListArtifactCatalog(context.Context, portalapi.Principal, portalapi.ManagementTarget, ArtifactCatalogQuery) (ArtifactCatalogPage, error)
	ArtifactRepositoryCapacity(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactCapacity, error)
	ListArtifactReferences(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactReferencePage, error)
	PlanArtifactGarbageCollection(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactGCPlan, error)
	ArtifactGarbageCollectionStatus(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactGCStatus, error)
	QuarantineArtifacts(context.Context, portalapi.Principal, portalapi.ManagementTarget, QuarantineArtifactsRequest) (ArtifactGCStatus, error)
	SweepArtifacts(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactGCStatus, error)
	SetArtifactLifecycle(context.Context, portalapi.Principal, portalapi.ManagementTarget, ArtifactLifecycleRequest) (ArtifactLifecycleResult, error)
	ArtifactMigrationStatus(context.Context, portalapi.Principal, portalapi.ManagementTarget) (ArtifactRepositoryMigration, error)
	PrepareArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, PrepareArtifactMigrationRequest) (ArtifactRepositoryMigration, error)
	SyncArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (ArtifactRepositoryMigration, error)
	CutoverArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, string, CutoverArtifactMigrationRequest) (ArtifactRepositoryMigration, error)
	RollbackArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (ArtifactRepositoryMigration, error)
	FinalizeArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (ArtifactRepositoryMigration, error)
	ReleaseArtifactMigration(context.Context, portalapi.Principal, portalapi.ManagementTarget, string) (ArtifactRepositoryMigration, error)
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
