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
	Capability                = "foundation.data.relational.runtime"
	RuntimePluginID           = "cn.vastplan.foundation.data.relational.runtime"
	ConnectionManagerPluginID = "cn.vastplan.platform.data.relational.connection-manager"
	CredentialPurpose         = "database.connection"

	OperationProviders = "providers"
	OperationProbe     = "probe"
	OperationActivate  = "activate"
	OperationRetire    = "retire"
	OperationQuery     = "query"
	OperationExecute   = "execute"
	OperationBegin     = "begin"
	OperationCommit    = "commit"
	OperationRollback  = "rollback"

	ErrorInvalidRequest        = "database.runtime.invalid_request"
	ErrorProviderNotFound      = "database.runtime.provider_not_found"
	ErrorUnsupported           = "database.runtime.unsupported"
	ErrorConnectionNotFound    = "database.runtime.connection_not_found"
	ErrorConnectionUnavailable = "database.runtime.connection_unavailable"
	ErrorPoolExhausted         = "database.runtime.pool_exhausted"
	ErrorDeadlineExceeded      = "database.runtime.deadline_exceeded"
	ErrorQueryFailed           = "database.runtime.query_failed"
	ErrorTransactionLost       = "database.runtime.transaction_lost"
	ErrorTransactionExpired    = "database.runtime.transaction_expired"
	ErrorTransactionConflict   = "database.runtime.transaction_conflict"
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

type ConnectionSpec struct {
	Ref         ConnectionRef                 `json:"ref"`
	ProviderID  string                        `json:"providerId"`
	Endpoint    string                        `json:"endpoint"`
	Database    string                        `json:"database,omitempty"`
	Options     json.RawMessage               `json:"options"`
	Credentials commonv1.ManagedCredentialRef `json:"credentials"`
	Pool        PoolPolicy                    `json:"pool"`
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
