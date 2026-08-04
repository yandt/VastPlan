// Package datamigrationv1 defines destructive schema migrations delivered as
// immutable files inside a signed plugin artifact. The manifest digest and the
// verified artifact signature form the trust chain; Runtime never accepts an
// unsigned ad-hoc SQL document from a caller.
package datamigrationv1

const (
	SchemaURL       = "https://schemas.cdsoft.com.cn/vastplan/datamigration/v1/vastplan.data-migration.schema.json"
	ContractVersion = "1.0.0"
)

type ModelIdentity struct {
	SchemaVersion uint64 `json:"schemaVersion"`
	SHA256        string `json:"sha256"`
}

type ProviderPlan struct {
	ProviderID string   `json:"providerId"`
	Statements []string `json:"statements"`
}

type Migration struct {
	Contract         string         `json:"contract"`
	ID               string         `json:"id"`
	ModelID          string         `json:"modelId"`
	From             ModelIdentity  `json:"from"`
	To               ModelIdentity  `json:"to"`
	RequiresBackup   bool           `json:"requiresBackup"`
	RequiresApproval bool           `json:"requiresApproval"`
	RetrySafe        bool           `json:"retrySafe"`
	Providers        []ProviderPlan `json:"providers"`
}

func (m Migration) Plan(providerID string) (ProviderPlan, bool) {
	for _, plan := range m.Providers {
		if plan.ProviderID == providerID {
			return plan, true
		}
	}
	return ProviderPlan{}, false
}
