// Package configurationresourcev1 defines the language-neutral controller
// protocol for independently versioned plugin configuration resources.
package configurationresourcev1

import (
	"encoding/json"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

const (
	SchemaURL      = "https://schemas.cdsoft.com.cn/vastplan/configuration-resource/v1/vastplan.configuration-resource-controller.schema.json"
	Protocol       = "configuration.resource.v1"
	ExtensionPoint = "configuration.resource-controller"

	OperationList    = "list"
	OperationGet     = "get"
	OperationPrepare = "prepare"
	OperationCommit  = "commit"
	OperationAbort   = "abort"
	OperationStatus  = "status"
)

type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
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

type ListRequest struct {
	CollectionID string `json:"collectionId"`
	Cursor       string `json:"cursor,omitempty"`
	Limit        uint32 `json:"limit,omitempty"`
}

type GetRequest struct {
	CollectionID string `json:"collectionId"`
	ResourceID   string `json:"resourceId"`
}

type PrepareRequest struct {
	CandidateID        string                                   `json:"candidateId"`
	ConfigurationID    string                                   `json:"configurationId"`
	CollectionID       string                                   `json:"collectionId"`
	ResourceID         string                                   `json:"resourceId"`
	Action             Action                                   `json:"action"`
	CatalogDigest      string                                   `json:"catalogDigest"`
	SchemaDigest       string                                   `json:"schemaDigest"`
	ArtifactSHA256     string                                   `json:"artifactSha256"`
	ExpectedActive     *ActiveReference                         `json:"expectedActive,omitempty"`
	Values             json.RawMessage                          `json:"values,omitempty"`
	ManagedCredentials map[string]commonv1.ManagedCredentialRef `json:"managedCredentials,omitempty"`
}

type CandidateRequest struct {
	CandidateID   string `json:"candidateId"`
	RequestDigest string `json:"requestDigest"`
}

type StatusRequest struct {
	CollectionID  string `json:"collectionId"`
	ResourceID    string `json:"resourceId"`
	CandidateID   string `json:"candidateId,omitempty"`
	RequestDigest string `json:"requestDigest,omitempty"`
}

type CredentialState struct {
	FieldID    string `json:"fieldId"`
	Configured bool   `json:"configured"`
	Version    int64  `json:"version,omitempty"`
}

// ResourceView contains signed-schema non-sensitive values only. Credential
// handles are represented as status metadata and never cross this boundary.
type ResourceView struct {
	ResourceID       string            `json:"resourceId"`
	Active           ActiveReference   `json:"active"`
	Values           json.RawMessage   `json:"values"`
	CredentialStates []CredentialState `json:"credentialStates,omitempty"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}

type ListResponse struct {
	Protocol     string         `json:"protocol"`
	CollectionID string         `json:"collectionId"`
	Items        []ResourceView `json:"items"`
	NextCursor   string         `json:"nextCursor,omitempty"`
	ObservedAt   time.Time      `json:"observedAt"`
}

type GetResponse struct {
	Protocol     string       `json:"protocol"`
	CollectionID string       `json:"collectionId"`
	Item         ResourceView `json:"item"`
	ObservedAt   time.Time    `json:"observedAt"`
}

type CandidateObservation struct {
	CandidateID   string          `json:"candidateId"`
	RequestDigest string          `json:"requestDigest"`
	ResultDigest  string          `json:"resultDigest"`
	Action        Action          `json:"action"`
	Status        CandidateStatus `json:"status"`
	Ready         bool            `json:"ready"`
	ErrorCode     string          `json:"errorCode,omitempty"`
	ErrorMessage  string          `json:"errorMessage,omitempty"`
}

type Observation struct {
	Protocol     string                `json:"protocol"`
	CollectionID string                `json:"collectionId"`
	ResourceID   string                `json:"resourceId"`
	Active       *ActiveReference      `json:"active,omitempty"`
	Candidate    *CandidateObservation `json:"candidate,omitempty"`
	ObservedAt   time.Time             `json:"observedAt"`
}
