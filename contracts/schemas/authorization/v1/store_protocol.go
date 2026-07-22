package authorizationv1

import (
	"encoding/json"
	"time"
)

type StoreProbeRequest struct{}

type StoreProbeResult struct {
	Ready            bool   `json:"ready"`
	ProviderID       string `json:"providerId"`
	ObservedRevision uint64 `json:"observedRevision"`
}

// StoreDocument is opaque to the Store Provider. The authorization-policy
// plugin owns format validation and meaning before compare-and-swap.
type StoreDocument struct {
	Format        string          `json:"format"`
	SchemaVersion string          `json:"schemaVersion"`
	Revision      uint64          `json:"revision"`
	Digest        string          `json:"digest"`
	Content       json.RawMessage `json:"content"`
}

type StoreLoadRequest struct {
	DomainID    string `json:"domainId"`
	MinRevision uint64 `json:"minRevision"`
}

type StoreLoadResult struct {
	Found    bool           `json:"found"`
	Document *StoreDocument `json:"document,omitempty"`
	ETag     string         `json:"etag,omitempty"`
}

type StoreCompareAndSwapRequest struct {
	DomainID         string        `json:"domainId"`
	ExpectedRevision uint64        `json:"expectedRevision"`
	ExpectedETag     string        `json:"expectedEtag,omitempty"`
	IdempotencyKey   string        `json:"idempotencyKey"`
	Document         StoreDocument `json:"document"`
}

type StoreCompareAndSwapResult struct {
	Applied  bool   `json:"applied"`
	Revision uint64 `json:"revision"`
	ETag     string `json:"etag"`
}

type StoreWatchRequest struct {
	DomainID string `json:"domainId"`
	Cursor   string `json:"cursor,omitempty"`
	Limit    int    `json:"limit"`
}

type StoreChange struct {
	Revision  uint64    `json:"revision"`
	Digest    string    `json:"digest"`
	Cursor    string    `json:"cursor"`
	ChangedAt time.Time `json:"changedAt"`
}

type StoreWatchResult struct {
	Changes    []StoreChange `json:"changes"`
	NextCursor string        `json:"nextCursor"`
}

type AuditRecord struct {
	EventID        string          `json:"eventId"`
	DomainID       string          `json:"domainId"`
	PolicyRevision uint64          `json:"policyRevision"`
	Action         string          `json:"action"`
	Subject        Subject         `json:"subject"`
	OccurredAt     time.Time       `json:"occurredAt"`
	Details        json.RawMessage `json:"details"`
}

type StoreAppendAuditRequest struct {
	IdempotencyKey string      `json:"idempotencyKey"`
	Record         AuditRecord `json:"record"`
}

type StoreAppendAuditResult struct {
	Accepted      bool   `json:"accepted"`
	AuditRevision uint64 `json:"auditRevision"`
}

type StoreBackupRequest struct {
	DomainID string `json:"domainId"`
	Revision uint64 `json:"revision"`
}

type StoreBackupResult struct {
	ArtifactURI string    `json:"artifactUri"`
	SHA256      string    `json:"sha256"`
	SizeBytes   int64     `json:"sizeBytes"`
	CreatedAt   time.Time `json:"createdAt"`
}
