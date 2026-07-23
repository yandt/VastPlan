// Package platformprofileactivation defines the narrow, non-secret boundary
// between deployment-manager and the trusted Backend kernel for platform-owned
// restart configuration. Catalog and Platform Profile documents never cross
// this boundary.
package platformprofileactivation

import (
	"context"
	"encoding/json"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

const (
	KernelPrepareService  = "kernel.platform-profile.prepare"
	KernelStatusService   = "kernel.platform-profile.status"
	KernelActivateService = "kernel.platform-profile.activate"
	KernelPublishService  = "kernel.platform-profile.publish"
	KernelFinalizeService = "kernel.platform-profile.finalize"
	KernelAbortService    = "kernel.platform-profile.abort"
	KernelRollbackService = "kernel.platform-profile.rollback"
)

type Status string

const (
	StatusPrepared   Status = "Prepared"
	StatusActivated  Status = "Activated"
	StatusFinalized  Status = "Finalized"
	StatusAborted    Status = "Aborted"
	StatusRolledBack Status = "RolledBack"
)

// PrepareRequest carries only the exact configuration candidate and the
// Application Composition already owned by deployment-manager. The kernel
// resolves the active Catalog, Profile, target service and signed definition.
type PrepareRequest struct {
	CandidateID         string                                       `json:"candidateId"`
	ConfigurationID     string                                       `json:"configurationId"`
	ConfigCatalogDigest string                                       `json:"configCatalogDigest"`
	SchemaDigest        string                                       `json:"schemaDigest"`
	ArtifactSHA256      string                                       `json:"artifactSha256"`
	Values              json.RawMessage                              `json:"values"`
	Credentials         map[string]pluginconfig.ManagedCredentialRef `json:"credentials,omitempty"`
	Composition         backendcompositionv1.ApplicationComposition  `json:"composition"`
	DeploymentRevision  uint64                                       `json:"deploymentRevision"`
}

// CandidateRequest is safe to persist in deployment-manager. RequestDigest
// binds every normalized PrepareRequest field and prevents a candidate ID from
// being replayed with a different composition or configuration.
type CandidateRequest struct {
	CandidateID   string `json:"candidateId"`
	RequestDigest string `json:"requestDigest"`
}

type PublishRequest struct {
	Prepare        PrepareRequest `json:"prepare"`
	RequestDigest  string         `json:"requestDigest"`
	ExpectedDigest string         `json:"expectedDigest"`
}

// Candidate exposes only immutable identities and recovery state. The full
// Profile and Catalog remain kernel-private.
type Candidate struct {
	CandidateID           string                  `json:"candidateId"`
	RequestDigest         string                  `json:"requestDigest"`
	ConfigurationID       string                  `json:"configurationId"`
	Deployment            string                  `json:"deployment"`
	PreviousProfile       compositioncommonv1.Ref `json:"previousProfile"`
	NextProfile           compositioncommonv1.Ref `json:"nextProfile"`
	ExpectedCatalogDigest string                  `json:"expectedCatalogDigest"`
	NextCatalogDigest     string                  `json:"nextCatalogDigest"`
	RollbackCatalogDigest string                  `json:"rollbackCatalogDigest,omitempty"`
	Status                Status                  `json:"status"`
}

type PrepareResult struct {
	Candidate Candidate                    `json:"candidate"`
	Preview   deploymentpublication.Result `json:"preview"`
}

type Controller interface {
	Prepare(context.Context, string, PrepareRequest) (PrepareResult, error)
	Status(context.Context, string, CandidateRequest) (Candidate, error)
	Activate(context.Context, string, CandidateRequest) (Candidate, error)
	Publish(context.Context, string, PublishRequest) (deploymentpublication.Result, error)
	Finalize(context.Context, string, CandidateRequest) (Candidate, error)
	Abort(context.Context, string, CandidateRequest) (Candidate, error)
	Rollback(context.Context, string, CandidateRequest) (Candidate, error)
}
