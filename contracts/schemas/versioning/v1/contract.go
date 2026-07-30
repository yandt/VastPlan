// Package versioningv1 defines the language-neutral JSON wire contract for
// immutable configuration versions and pluggable Version Ledger providers.
package versioningv1

import (
	"encoding/json"
	"time"
)

const (
	SchemaURL          = "https://schemas.cdsoft.com.cn/vastplan/versioning/v1/vastplan.version-ledger.schema.json"
	Protocol           = "version.ledger.v1"
	LedgerCapability   = "foundation.versioning.ledger"
	ProviderCapability = "foundation.versioning.provider"

	StorageProtocolFile       = "version.storage.file.v1"
	StorageProtocolGit        = "version.storage.git.v1"
	StorageProtocolRelational = "version.storage.relational.v1"

	ConsistencySingleWriter = "single-writer"
	ConsistencyRefCAS       = "ref-cas"
	ConsistencyLinearizable = "linearizable"
	DurabilityLocal         = "local"
	DurabilityShared        = "shared"

	OperationProviders   = "providers"
	OperationPutVersion  = "putVersion"
	OperationGetVersion  = "getVersion"
	OperationListHistory = "listHistory"
	OperationGetHead     = "getHead"
	OperationMoveHead    = "moveHead"

	ProviderOperationDescribe   = "describe"
	ProviderOperationPutVersion = OperationPutVersion
	ProviderOperationGetVersion = OperationGetVersion
	ProviderOperationHistory    = OperationListHistory
	ProviderOperationGetHead    = OperationGetHead
	ProviderOperationMoveHead   = OperationMoveHead

	ErrorInvalidRequest      = "version.ledger.invalid_request"
	ErrorProviderNotFound    = "version.ledger.provider_not_found"
	ErrorProviderUnavailable = "version.ledger.provider_unavailable"
	ErrorNotFound            = "version.ledger.not_found"
	ErrorConflict            = "version.ledger.conflict"
	ErrorDigestMismatch      = "version.ledger.digest_mismatch"
	ErrorCorrupted           = "version.ledger.corrupted"
	ErrorLimitExceeded       = "version.ledger.limit_exceeded"
	ErrorUnsupported         = "version.ledger.unsupported"

	MaxContentBytes = 1 << 20
	MaxHistoryPage  = 200
)

type ProviderCapabilities struct {
	DetachedVersions bool `json:"detachedVersions"`
	NamedHeads       bool `json:"namedHeads"`
	StableHistory    bool `json:"stableHistory"`
}

type ProviderDescriptor struct {
	ID                  string               `json:"id"`
	Protocol            string               `json:"protocol"`
	Version             string               `json:"version"`
	DisplayName         string               `json:"displayName"`
	Consistency         string               `json:"consistency"`
	Durability          string               `json:"durability"`
	ClusterSafe         bool                 `json:"clusterSafe"`
	MaxContentBytes     int                  `json:"maxContentBytes"`
	ConfigurationSchema json.RawMessage      `json:"configurationSchema"`
	Capabilities        ProviderCapabilities `json:"capabilities"`
}

type StreamKey struct {
	Namespace string `json:"namespace"`
	StreamID  string `json:"streamId"`
}

type VersionRef struct {
	Stream        StreamKey `json:"stream"`
	VersionID     string    `json:"versionId"`
	Sequence      uint64    `json:"sequence"`
	ContentDigest string    `json:"contentDigest"`
}

type VersionRecord struct {
	Protocol  string            `json:"protocol"`
	Ref       VersionRef        `json:"ref"`
	Parent    *VersionRef       `json:"parent,omitempty"`
	Content   json.RawMessage   `json:"content"`
	Message   string            `json:"message,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	ActorID   string            `json:"actorId"`
	CreatedAt time.Time         `json:"createdAt"`
}

type Head struct {
	Protocol  string     `json:"protocol"`
	Stream    StreamKey  `json:"stream"`
	Name      string     `json:"name"`
	Target    VersionRef `json:"target"`
	Revision  uint64     `json:"revision"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

type ProviderListRequest struct{}
type ProviderListResult struct {
	Providers []ProviderDescriptor `json:"providers"`
}

type PutVersionRequest struct {
	Stream         StreamKey         `json:"stream"`
	IdempotencyKey string            `json:"idempotencyKey"`
	Parent         *VersionRef       `json:"parent,omitempty"`
	Content        json.RawMessage   `json:"content"`
	Message        string            `json:"message,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

type PutVersionResult struct {
	Version VersionRecord `json:"version"`
	Reused  bool          `json:"reused"`
}

type GetVersionRequest struct {
	Ref VersionRef `json:"ref"`
}

type GetVersionResult struct {
	Version VersionRecord `json:"version"`
}

type ListHistoryRequest struct {
	Stream StreamKey   `json:"stream"`
	Start  *VersionRef `json:"start,omitempty"`
	Limit  int         `json:"limit"`
	Cursor string      `json:"cursor,omitempty"`
}

type ListHistoryResult struct {
	Versions   []VersionRecord `json:"versions"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

type GetHeadRequest struct {
	Stream StreamKey `json:"stream"`
	Name   string    `json:"name"`
}

type GetHeadResult struct {
	Head Head `json:"head"`
}

type MoveHeadRequest struct {
	Stream           StreamKey  `json:"stream"`
	Name             string     `json:"name"`
	Target           VersionRef `json:"target"`
	ExpectedRevision uint64     `json:"expectedRevision"`
}

type MoveHeadResult struct {
	Head Head `json:"head"`
}

// ProviderVersionCandidate contains canonical content and the trusted actor
// projected by Ledger. The Provider atomically assigns sequence, version ID
// and creation time with the durable write.
type ProviderVersionCandidate struct {
	Stream  StreamKey         `json:"stream"`
	Parent  *VersionRef       `json:"parent,omitempty"`
	Content json.RawMessage   `json:"content"`
	Message string            `json:"message,omitempty"`
	Labels  map[string]string `json:"labels,omitempty"`
	ActorID string            `json:"actorId"`
}

type ProviderPutVersionRequest struct {
	IdempotencyKey string                   `json:"idempotencyKey"`
	Candidate      ProviderVersionCandidate `json:"candidate"`
}

type ProviderPutVersionResult = PutVersionResult
type ProviderDescribeRequest struct{}
type ProviderDescribeResult struct {
	Provider ProviderDescriptor `json:"provider"`
}
