// Package recordstorev1 defines the language-neutral wire contract for the
// declarative Record Store capability. Business workflows and public APIs are
// intentionally outside this package.
package recordstorev1

import (
	"encoding/json"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

const (
	SchemaURL       = "https://schemas.cdsoft.com.cn/vastplan/recordstore/v1/vastplan.record-store.schema.json"
	Capability      = "foundation.data.record-store"
	ContractVersion = "1.0.0"

	OperationSyncModels   = "syncModels"
	OperationCreate       = "create"
	OperationGet          = "get"
	OperationList         = "list"
	OperationUpdate       = "update"
	OperationDelete       = "delete"
	OperationBatch        = "batch"
	OperationBegin        = "begin"
	OperationCommit       = "commit"
	OperationRollback     = "rollback"
	OperationAppendOutbox = "appendOutbox"

	ErrorInvalidRequest  = "record.store.invalid_request"
	ErrorModelNotFound   = "record.store.model_not_found"
	ErrorModelMismatch   = "record.store.model_mismatch"
	ErrorNotFound        = "record.store.not_found"
	ErrorConflict        = "record.store.conflict"
	ErrorAlreadyExists   = "record.store.already_exists"
	ErrorStorageDenied   = "record.store.storage_denied"
	ErrorMigrationNeeded = "record.store.migration_needed"
	ErrorUnavailable     = "record.store.unavailable"
)

type ModelRef struct {
	ID            string `json:"id"`
	SchemaVersion uint64 `json:"schemaVersion"`
	SHA256        string `json:"sha256"`
}

type SignedModel struct {
	OwnerPluginID  string   `json:"ownerPluginId"`
	Model          ModelRef `json:"model"`
	DocumentBase64 string   `json:"documentBase64"`
}

type SyncModelsRequest struct {
	Generation uint64        `json:"generation"`
	Models     []SignedModel `json:"models"`
}

type SyncModelsResult struct {
	Generation uint64 `json:"generation"`
	Models     int    `json:"models"`
}

// StorageTarget is empty for a reserved platform-control model. A normal
// connection-ref model must carry an exact active connection revision.
type StorageTarget struct {
	Connection *databasev1.ConnectionRef `json:"connection,omitempty"`
}

type Record map[string]json.RawMessage
type Key map[string]json.RawMessage

type Filter struct {
	Field    string            `json:"field"`
	Operator string            `json:"operator"`
	Value    json.RawMessage   `json:"value,omitempty"`
	Values   []json.RawMessage `json:"values,omitempty"`
}

type Sort struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

type CreateRequest struct {
	Storage           StorageTarget `json:"storage"`
	Model             ModelRef      `json:"model"`
	Record            Record        `json:"record"`
	IdempotencyKey    string        `json:"idempotencyKey"`
	TransactionHandle string        `json:"transactionHandle,omitempty"`
}

type GetRequest struct {
	Storage           StorageTarget `json:"storage"`
	Model             ModelRef      `json:"model"`
	Key               Key           `json:"key"`
	TransactionHandle string        `json:"transactionHandle,omitempty"`
}

type ListRequest struct {
	Storage           StorageTarget `json:"storage"`
	Model             ModelRef      `json:"model"`
	Filters           []Filter      `json:"filters"`
	Sort              []Sort        `json:"sort"`
	Limit             int           `json:"limit"`
	Cursor            string        `json:"cursor,omitempty"`
	TransactionHandle string        `json:"transactionHandle,omitempty"`
}

type UpdateRequest struct {
	Storage           StorageTarget `json:"storage"`
	Model             ModelRef      `json:"model"`
	Key               Key           `json:"key"`
	Values            Record        `json:"values"`
	ExpectedRevision  int64         `json:"expectedRevision"`
	IdempotencyKey    string        `json:"idempotencyKey"`
	TransactionHandle string        `json:"transactionHandle,omitempty"`
}

type DeleteRequest struct {
	Storage           StorageTarget `json:"storage"`
	Model             ModelRef      `json:"model"`
	Key               Key           `json:"key"`
	ExpectedRevision  int64         `json:"expectedRevision"`
	IdempotencyKey    string        `json:"idempotencyKey"`
	TransactionHandle string        `json:"transactionHandle,omitempty"`
}

type Mutation struct {
	Kind             string `json:"kind"`
	Record           Record `json:"record,omitempty"`
	Key              Key    `json:"key,omitempty"`
	Values           Record `json:"values,omitempty"`
	ExpectedRevision int64  `json:"expectedRevision,omitempty"`
	IdempotencyKey   string `json:"idempotencyKey"`
}

type BatchRequest struct {
	Storage           StorageTarget `json:"storage"`
	Model             ModelRef      `json:"model"`
	Mutations         []Mutation    `json:"mutations"`
	TransactionHandle string        `json:"transactionHandle,omitempty"`
}

type RecordResult struct {
	Record Record `json:"record"`
}

type ListResult struct {
	Records    []Record `json:"records"`
	NextCursor string   `json:"nextCursor,omitempty"`
}

type MutationResult struct {
	Kind   string `json:"kind"`
	Record Record `json:"record,omitempty"`
}

type BatchResult struct {
	Results []MutationResult `json:"results"`
}

type BeginRequest struct {
	Storage StorageTarget                 `json:"storage"`
	Model   ModelRef                      `json:"model"`
	Options databasev1.TransactionOptions `json:"options"`
}

type BeginResult = databasev1.BeginResult

type EndRequest struct {
	TransactionHandle string `json:"transactionHandle"`
}

type AppendOutboxRequest struct {
	Storage           StorageTarget   `json:"storage"`
	Model             ModelRef        `json:"model"`
	Topic             string          `json:"topic"`
	Payload           json.RawMessage `json:"payload"`
	IdempotencyKey    string          `json:"idempotencyKey"`
	TransactionHandle string          `json:"transactionHandle,omitempty"`
}
