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
	ContractVersion = "1.2.0"
	// TrustedInventoryConfigKey is reserved for the controller-projected,
	// content-bound model inventory. It is injected after user configuration
	// validation and must never be accepted from a plugin Profile.
	TrustedInventoryConfigKey      = "_hostDataModelInventory"
	StorageBindingsConfigKey       = "recordStoreBindings"
	SchemaControllerEvidencePrefix = "database.schema-controller/"

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
	OperationSchemaPlan   = "schemaPlan"
	OperationSchemaApply  = "schemaApply"
	OperationSchemaStatus = "schemaStatus"

	ErrorInvalidRequest     = "record.store.invalid_request"
	ErrorModelNotFound      = "record.store.model_not_found"
	ErrorModelMismatch      = "record.store.model_mismatch"
	ErrorNotFound           = "record.store.not_found"
	ErrorConflict           = "record.store.conflict"
	ErrorAlreadyExists      = "record.store.already_exists"
	ErrorStorageDenied      = "record.store.storage_denied"
	ErrorMigrationNeeded    = "record.store.migration_needed"
	ErrorUnavailable        = "record.store.unavailable"
	ErrorTransactionLost    = "record.store.transaction_lost"
	ErrorTransactionExpired = "record.store.transaction_expired"
)

func SchemaControllerEvidence(resourceID string) string {
	return SchemaControllerEvidencePrefix + resourceID
}

type ModelRef struct {
	ID            string `json:"id"`
	SchemaVersion uint64 `json:"schemaVersion"`
	SHA256        string `json:"sha256"`
}

type SignedModel struct {
	OwnerPluginID  string   `json:"ownerPluginId"`
	ArtifactSHA256 string   `json:"artifactSha256"`
	Model          ModelRef `json:"model"`
	DocumentBase64 string   `json:"documentBase64"`
}

type MigrationRef struct {
	ID          string `json:"id"`
	ModelID     string `json:"modelId"`
	FromVersion uint64 `json:"fromVersion"`
	ToVersion   uint64 `json:"toVersion"`
	SHA256      string `json:"sha256"`
}

type SignedMigration struct {
	OwnerPluginID  string       `json:"ownerPluginId"`
	ArtifactSHA256 string       `json:"artifactSha256"`
	Migration      MigrationRef `json:"migration"`
	DocumentBase64 string       `json:"documentBase64"`
}

type SyncModelsRequest struct {
	Generation       uint64            `json:"generation"`
	InventoryDigest  string            `json:"inventoryDigest"`
	Models           []SignedModel     `json:"models"`
	Migrations       []SignedMigration `json:"migrations,omitempty"`
	SchemaActivation *SchemaActivation `json:"schemaActivation,omitempty"`
}

type SyncModelsResult struct {
	Generation uint64 `json:"generation"`
	Models     int    `json:"models"`
	Migrations int    `json:"migrations"`
}

// StorageTarget is empty for a reserved platform-control model. A normal
// connection-ref model must carry an exact active connection revision.
type StorageTarget struct {
	Connection *databasev1.ConnectionRef `json:"connection,omitempty"`
}

const (
	SchemaActivationApproved  = "approved"
	SchemaActivationAutomatic = "automatic"
)

// SchemaActivation is host-issued publication evidence. Plugins cannot place
// it in ordinary configuration; the deployment publisher binds it to the
// candidate inventory and Node Agent consumes it before publishing routes.
type SchemaActivation struct {
	CandidateID string                         `json:"candidateId"`
	PlanDigest  string                         `json:"planDigest"`
	Mode        string                         `json:"mode"`
	ApprovedBy  string                         `json:"approvedBy,omitempty"`
	Models      []SchemaMigrationAuthorization `json:"models"`
}

type SchemaMigrationAuthorization struct {
	Model       ModelRef      `json:"model"`
	Storage     StorageTarget `json:"storage"`
	Kind        string        `json:"kind"`
	MigrationID string        `json:"migrationId,omitempty"`
	AllowSafe   bool          `json:"allowSafe,omitempty"`
	AllowSigned bool          `json:"allowSigned,omitempty"`
	BackupRef   string        `json:"backupRef,omitempty"`
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

type AppendOutboxResult struct {
	ID string `json:"id"`
}

type SchemaRequest struct {
	Storage     StorageTarget `json:"storage"`
	Model       ModelRef      `json:"model"`
	MigrationID string        `json:"migrationId,omitempty"`
}

type SchemaPlanResult struct {
	Kind       string   `json:"kind"`
	Statements int      `json:"statements"`
	Reasons    []string `json:"reasons"`
}

type SchemaStatusResult struct {
	Ready         bool   `json:"ready"`
	SchemaVersion uint64 `json:"schemaVersion,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
}
