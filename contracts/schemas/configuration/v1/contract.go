// Package configurationv1 defines the language-neutral service hot
// configuration control protocol. The wire is JSON so Go, Node, Python and
// future runtime drivers can implement the same prepare/commit/abort/status
// state machine without importing one another.
package configurationv1

import (
	"encoding/json"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

const (
	SchemaURL      = "https://schemas.cdsoft.com.cn/vastplan/configuration/v1/vastplan.configuration-controller.schema.json"
	Protocol       = "configuration.v1"
	ExtensionPoint = "configuration.controller"

	OperationPrepare = "prepare"
	OperationCommit  = "commit"
	OperationAbort   = "abort"
	OperationStatus  = "status"
)

type CandidateStatus string

const (
	StatusPrepared  CandidateStatus = "Prepared"
	StatusCommitted CandidateStatus = "Committed"
	StatusAborted   CandidateStatus = "Aborted"
)

type ActiveReference struct {
	Revision uint64 `json:"revision"`
	Digest   string `json:"digest"`
}

type PrepareRequest struct {
	CandidateID        string                                   `json:"candidateId"`
	ConfigurationID    string                                   `json:"configurationId"`
	CatalogDigest      string                                   `json:"catalogDigest"`
	SchemaDigest       string                                   `json:"schemaDigest"`
	ArtifactSHA256     string                                   `json:"artifactSha256"`
	ExpectedActive     ActiveReference                          `json:"expectedActive"`
	Values             json.RawMessage                          `json:"values"`
	ManagedCredentials map[string]commonv1.ManagedCredentialRef `json:"managedCredentials,omitempty"`
}

type CandidateRequest struct {
	CandidateID   string `json:"candidateId"`
	RequestDigest string `json:"requestDigest"`
}

// StatusRequest without candidateId returns the controller's current Active
// reference. candidateId and requestDigest must either both be present or both
// be absent, preventing an unbound status lookup.
type StatusRequest struct {
	ConfigurationID string `json:"configurationId"`
	CandidateID     string `json:"candidateId,omitempty"`
	RequestDigest   string `json:"requestDigest,omitempty"`
}

type CandidateObservation struct {
	CandidateID         string          `json:"candidateId"`
	RequestDigest       string          `json:"requestDigest"`
	ConfigurationDigest string          `json:"configurationDigest"`
	Status              CandidateStatus `json:"status"`
	Ready               bool            `json:"ready"`
	ErrorCode           string          `json:"errorCode,omitempty"`
	ErrorMessage        string          `json:"errorMessage,omitempty"`
}

// Observation intentionally contains only digests and lifecycle facts. Values,
// managed credential handles and material never leave the target controller.
type Observation struct {
	Protocol        string                `json:"protocol"`
	ConfigurationID string                `json:"configurationId"`
	Active          ActiveReference       `json:"active"`
	Candidate       *CandidateObservation `json:"candidate,omitempty"`
	ObservedAt      time.Time             `json:"observedAt"`
}
