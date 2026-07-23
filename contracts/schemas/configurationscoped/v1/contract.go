// Package configurationscopedv1 defines the language-neutral read port for
// tenant- and user-scoped hot plugin configuration.
package configurationscopedv1

import (
	"encoding/json"
	"time"
)

const (
	SchemaURL      = "https://schemas.cdsoft.com.cn/vastplan/configuration-scoped/v1/vastplan.configuration-scoped.schema.json"
	Protocol       = "configuration.scoped.v1"
	ExtensionPoint = "configuration.scoped-resolver"
	Capability     = "configuration.scoped"

	OperationResolve       = "resolve"
	OperationWatchRevision = "watchRevision"
	MaxWatchTimeoutMS      = uint32(30_000)
)

type Scope string

const (
	ScopeTenant Scope = "tenant"
	ScopeUser   Scope = "user"
)

type ResolveRequest struct{}

type WatchRevisionRequest struct {
	AfterRevision uint64 `json:"afterRevision"`
	AfterDigest   string `json:"afterDigest"`
	TimeoutMS     uint32 `json:"timeoutMs,omitempty"`
}

// Resolution contains non-sensitive values validated against the exact
// signed schema. Tenant and subject identities are intentionally omitted: the
// resolver derives them from the trusted CallContext and never echoes them.
type Resolution struct {
	Protocol        string          `json:"protocol"`
	ConfigurationID string          `json:"configurationId"`
	Scope           Scope           `json:"scope"`
	Revision        uint64          `json:"revision"`
	Digest          string          `json:"digest"`
	SchemaDigest    string          `json:"schemaDigest"`
	ArtifactSHA256  string          `json:"artifactSha256"`
	Values          json.RawMessage `json:"values"`
	Source          string          `json:"source"`
	ObservedAt      time.Time       `json:"observedAt"`
}

// RevisionObservation is deliberately value-free. A watcher that observes a
// change must call resolve again so every value response repeats authorization,
// active-catalog and schema checks.
type RevisionObservation struct {
	Protocol        string    `json:"protocol"`
	ConfigurationID string    `json:"configurationId"`
	Changed         bool      `json:"changed"`
	Revision        uint64    `json:"revision"`
	Digest          string    `json:"digest"`
	ObservedAt      time.Time `json:"observedAt"`
}
