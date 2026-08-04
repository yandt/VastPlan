// Package datamodelv1 defines the signed, language-neutral data.model.v1 contract.
// It describes persistence shape only; API, authorization, workflows and UI are
// deliberately outside this package.
package datamodelv1

const (
	SchemaURL       = "https://schemas.cdsoft.com.cn/vastplan/datamodel/v1/vastplan.data-model.schema.json"
	ContractVersion = "1.0.0"
)

type Model struct {
	Contract          string             `json:"contract"`
	ID                string             `json:"id"`
	SchemaVersion     uint64             `json:"schemaVersion"`
	Storage           StorageBinding     `json:"storage"`
	Fields            []Field            `json:"fields"`
	PrimaryKey        []string           `json:"primaryKey"`
	Indexes           []Index            `json:"indexes"`
	UniqueConstraints []UniqueConstraint `json:"uniqueConstraints"`
	Scope             Scope              `json:"scope"`
	OptimisticLock    *OptimisticLock    `json:"optimisticLock,omitempty"`
	Audit             *AuditFields       `json:"audit,omitempty"`
	Deletion          DeletionPolicy     `json:"deletion"`
}

type StorageBinding struct {
	Kind  string `json:"kind"`
	Table string `json:"table"`
}

type Field struct {
	ID          string `json:"id"`
	Column      string `json:"column"`
	Type        string `json:"type"`
	Nullable    bool   `json:"nullable"`
	Sensitivity string `json:"sensitivity"`
}

type Index struct {
	ID     string   `json:"id"`
	Fields []string `json:"fields"`
	Unique bool     `json:"unique"`
}

type UniqueConstraint struct {
	ID     string   `json:"id"`
	Fields []string `json:"fields"`
}

type Scope struct {
	Tenant  string `json:"tenant"`
	Service string `json:"service"`
}

type OptimisticLock struct {
	Field string `json:"field"`
}

type AuditFields struct {
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	CreatedBy string `json:"createdBy,omitempty"`
	UpdatedBy string `json:"updatedBy,omitempty"`
}

type DeletionPolicy struct {
	Mode  string `json:"mode"`
	Field string `json:"field,omitempty"`
}
