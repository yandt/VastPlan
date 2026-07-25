package platformadminapi

import (
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

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
