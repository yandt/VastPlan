// Package databasev1 defines the language-neutral JSON wire contract used by
// the Database Runtime capability. It contains no driver or pool objects.
package databasev1

import (
	"encoding/json"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

const (
	SchemaURL                 = "https://schemas.cdsoft.com.cn/vastplan/database/v1/vastplan.database-runtime.schema.json"
	ContractVersion           = "1.3.0"
	Capability                = "foundation.data.relational.runtime"
	RuntimePluginID           = "cn.vastplan.foundation.data.relational.runtime"
	ConnectionManagerPluginID = "cn.vastplan.platform.data.relational.connection-manager"
	CredentialPurpose         = "database.connection"

	OperationProviders = "providers"
	OperationMetrics   = "metrics"
	OperationProbe     = "probe"
	OperationActivate  = "activate"
	OperationRetire    = "retire"
	OperationQuery     = "query"
	OperationExecute   = "execute"
	OperationBegin     = "begin"
	OperationCommit    = "commit"
	OperationRollback  = "rollback"

	ErrorInvalidRequest               = "database.runtime.invalid_request"
	ErrorProviderNotFound             = "database.runtime.provider_not_found"
	ErrorUnsupported                  = "database.runtime.unsupported"
	ErrorConnectionNotFound           = "database.runtime.connection_not_found"
	ErrorConnectionUnavailable        = "database.runtime.connection_unavailable"
	ErrorCredentialUnavailable        = "database.runtime.credential_unavailable"
	ErrorCredentialServiceUnavailable = "database.runtime.credential_service_unavailable"
	ErrorTLSPolicyForbidden           = "database.runtime.tls_policy_forbidden"
	ErrorNameResolutionFailed         = "database.runtime.name_resolution_failed"
	ErrorConnectionRefused            = "database.runtime.connection_refused"
	ErrorConnectionTimeout            = "database.runtime.connection_timeout"
	ErrorTLSVerificationFailed        = "database.runtime.tls_verification_failed"
	ErrorAuthenticationFailed         = "database.runtime.authentication_failed"
	ErrorDatabaseNotFound             = "database.runtime.database_not_found"
	ErrorPermissionDenied             = "database.runtime.permission_denied"
	ErrorPoolExhausted                = "database.runtime.pool_exhausted"
	ErrorDeadlineExceeded             = "database.runtime.deadline_exceeded"
	ErrorQueryFailed                  = "database.runtime.query_failed"
	ErrorTransactionLost              = "database.runtime.transaction_lost"
	ErrorTransactionExpired           = "database.runtime.transaction_expired"
	ErrorTransactionConflict          = "database.runtime.transaction_conflict"
	ErrorConstraintViolation          = "database.runtime.constraint_violation"
)

type ProviderCapabilities struct {
	Query                bool `json:"query"`
	Execute              bool `json:"execute"`
	Transactions         bool `json:"transactions"`
	ReadOnlyTransactions bool `json:"readOnlyTransactions,omitempty"`
	Savepoints           bool `json:"savepoints,omitempty"`
	Streaming            bool `json:"streaming,omitempty"`
	NamedParameters      bool `json:"namedParameters,omitempty"`
}

type ProviderDescriptor struct {
	ID                  string               `json:"id"`
	Version             string               `json:"version"`
	DisplayName         string               `json:"displayName"`
	ConfigurationSchema json.RawMessage      `json:"configurationSchema"`
	Capabilities        ProviderCapabilities `json:"capabilities"`
}

type ConnectionRef struct {
	ResourceID string `json:"resourceId"`
	Revision   uint64 `json:"revision"`
}

type PoolPolicy struct {
	MinIdle          int   `json:"minIdle,omitempty"`
	MaxIdle          int   `json:"maxIdle"`
	MaxOpen          int   `json:"maxOpen"`
	MaxLifetimeMS    int64 `json:"maxLifetimeMs"`
	MaxIdleTimeMS    int64 `json:"maxIdleTimeMs"`
	AcquireTimeoutMS int64 `json:"acquireTimeoutMs"`
	IdlePoolTTLMS    int64 `json:"idlePoolTtlMs"`
}

// ConnectionCandidate is the single non-secret connection input shared by
// managed connections and trusted bootstrap workflows. Credential and
// persistence lifecycles intentionally remain outside this value.
type ConnectionCandidate struct {
	ProviderID string          `json:"providerId"`
	Endpoint   string          `json:"endpoint"`
	Database   string          `json:"database,omitempty"`
	Options    json.RawMessage `json:"options"`
	Pool       PoolPolicy      `json:"pool"`
}

func DefaultPoolPolicy() PoolPolicy {
	return PoolPolicy{
		MinIdle: 0, MaxIdle: 8, MaxOpen: 32, MaxLifetimeMS: 30 * 60_000,
		MaxIdleTimeMS: 5 * 60_000, AcquireTimeoutMS: 5_000, IdlePoolTTLMS: 15 * 60_000,
	}
}

type ConnectionSpec struct {
	Ref         ConnectionRef                 `json:"ref"`
	ProviderID  string                        `json:"providerId"`
	Endpoint    string                        `json:"endpoint"`
	Database    string                        `json:"database,omitempty"`
	Options     json.RawMessage               `json:"options"`
	Credentials commonv1.ManagedCredentialRef `json:"credentials"`
	Pool        PoolPolicy                    `json:"pool"`
}

func (s ConnectionSpec) Candidate() ConnectionCandidate {
	return ConnectionCandidate{
		ProviderID: s.ProviderID, Endpoint: s.Endpoint, Database: s.Database,
		Options: append(json.RawMessage(nil), s.Options...), Pool: s.Pool,
	}
}

type Value struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value,omitempty"`
}

type Statement struct {
	SQL        string  `json:"sql"`
	Parameters []Value `json:"parameters"`
}

type TransactionOptions struct {
	Isolation string `json:"isolation"`
	ReadOnly  bool   `json:"readOnly"`
	TimeoutMS int64  `json:"timeoutMs"`
}

type ProviderListRequest struct{}
type ProviderListResult struct {
	Providers []ProviderDescriptor `json:"providers"`
}

// MetricsRequest intentionally has no filter: connection-scoped filters would
// create a high-cardinality observability API and can reveal tenant topology.
type MetricsRequest struct{}

type RuntimeHealth struct {
	Status                 string `json:"status"`
	ActiveGenerations      uint64 `json:"activeGenerations"`
	HealthyGenerations     uint64 `json:"healthyGenerations"`
	UnhealthyGenerations   uint64 `json:"unhealthyGenerations"`
	DrainingGenerations    uint64 `json:"drainingGenerations"`
	CloseFailedGenerations uint64 `json:"closeFailedGenerations"`
}

type PoolMetrics struct {
	OpenConnections    uint64 `json:"openConnections"`
	IdleConnections    uint64 `json:"idleConnections"`
	InUseConnections   uint64 `json:"inUseConnections"`
	MaxOpenConnections uint64 `json:"maxOpenConnections"`
	Waiting            uint64 `json:"waiting"`
	InFlight           uint64 `json:"inFlight"`
	WaitCount          uint64 `json:"waitCount"`
	WaitDurationMS     uint64 `json:"waitDurationMs"`
	NodeReserved       uint64 `json:"nodeReserved"`
	BudgetRejected     uint64 `json:"budgetRejected"`
	AcquireSucceeded   uint64 `json:"acquireSucceeded"`
	AcquireTimeouts    uint64 `json:"acquireTimeouts"`
	QueueRejected      uint64 `json:"queueRejected"`
	ForcedDrains       uint64 `json:"forcedDrains"`
	CloseFailures      uint64 `json:"closeFailures"`
}

type TransactionMetrics struct {
	Active    uint64 `json:"active"`
	Capacity  uint64 `json:"capacity"`
	Begins    uint64 `json:"begins"`
	Commits   uint64 `json:"commits"`
	Rollbacks uint64 `json:"rollbacks"`
	Expired   uint64 `json:"expired"`
	Lost      uint64 `json:"lost"`
	Rejected  uint64 `json:"rejected"`
}

// MetricSample follows Prometheus/OpenTelemetry counter/gauge conventions.
// Labels are deliberately limited to the low-cardinality provider value.
type MetricSample struct {
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Unit   string            `json:"unit"`
	Value  uint64            `json:"value"`
	Labels map[string]string `json:"labels,omitempty"`
}

type RuntimeMetricsResult struct {
	SchemaVersion int                `json:"schemaVersion"`
	ObservedAt    time.Time          `json:"observedAt"`
	Health        RuntimeHealth      `json:"health"`
	Pools         PoolMetrics        `json:"pools"`
	Transactions  TransactionMetrics `json:"transactions"`
	Samples       []MetricSample     `json:"samples"`
}

type ProbeRequest struct {
	Connection ConnectionSpec `json:"connection"`
}
type ProbeResult struct {
	Ready      bool   `json:"ready"`
	ProviderID string `json:"providerId"`
	LatencyMS  int64  `json:"latencyMs"`
	Message    string `json:"message,omitempty"`
}

type ActivateRequest struct {
	Connection ConnectionSpec `json:"connection"`
}
type ActivateResult struct {
	Connection ConnectionRef `json:"connection"`
	Generation uint64        `json:"generation"`
	Ready      bool          `json:"ready"`
}

type RetireRequest struct {
	Connection ConnectionRef `json:"connection"`
}

type QueryRequest struct {
	Connection        ConnectionRef `json:"connection"`
	Statement         Statement     `json:"statement"`
	TransactionHandle string        `json:"transactionHandle,omitempty"`
	MaxRows           int           `json:"maxRows"`
}

type ExecuteRequest struct {
	Connection        ConnectionRef `json:"connection"`
	Statement         Statement     `json:"statement"`
	TransactionHandle string        `json:"transactionHandle,omitempty"`
}

type Column struct {
	Name         string `json:"name"`
	DatabaseType string `json:"databaseType"`
	Nullable     bool   `json:"nullable"`
}

type QueryResult struct {
	Columns   []Column  `json:"columns"`
	Rows      [][]Value `json:"rows"`
	Truncated bool      `json:"truncated"`
}

type ExecuteResult struct {
	RowsAffected int64 `json:"rowsAffected"`
}

type BeginRequest struct {
	Connection ConnectionRef      `json:"connection"`
	Options    TransactionOptions `json:"options"`
}

type BeginResult struct {
	TransactionHandle string    `json:"transactionHandle"`
	ExpiresAt         time.Time `json:"expiresAt"`
}

type EndTransactionRequest struct {
	TransactionHandle string `json:"transactionHandle"`
}
