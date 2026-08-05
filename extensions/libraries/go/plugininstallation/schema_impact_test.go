package plugininstallation

import (
	"strings"
	"testing"

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
)

func TestBuildSchemaImpactClassifiesSafeAndSignedChanges(t *testing.T) {
	base := datamodelv1.Model{Contract: "data.model.v1", ID: "example.order", SchemaVersion: 1,
		Storage: datamodelv1.StorageBinding{Kind: "platform-control", Table: "orders"},
		Fields:  []datamodelv1.Field{{ID: "id", Column: "id", Type: "uuid", Sensitivity: "internal"}}, PrimaryKey: []string{"id"},
		Indexes: []datamodelv1.Index{}, UniqueConstraints: []datamodelv1.UniqueConstraint{}, Scope: datamodelv1.Scope{Tenant: "none", Service: "none"}, Deletion: datamodelv1.DeletionPolicy{Mode: "hard"}}
	current := deploymentpublication.DataModelCatalog{Digest: strings.Repeat("a", 64), Models: []deploymentpublication.DataModelDescriptor{{OwnerPluginID: "cn.example", Ref: recordstorev1.ModelRef{ID: base.ID, SchemaVersion: 1, SHA256: strings.Repeat("b", 64)}, Model: base}}}
	next := base
	next.SchemaVersion = 2
	next.Fields = append(next.Fields, datamodelv1.Field{ID: "note", Column: "note", Type: "string", Nullable: true, Sensitivity: "internal"})
	target := deploymentpublication.DataModelCatalog{Digest: strings.Repeat("c", 64), Models: []deploymentpublication.DataModelDescriptor{{OwnerPluginID: "cn.example", Ref: recordstorev1.ModelRef{ID: next.ID, SchemaVersion: 2, SHA256: strings.Repeat("d", 64)}, Model: next}}}
	impact := BuildSchemaImpact(current, target)
	if !impact.RequiresMigration || !impact.RequiresConfirmation || impact.RequiresBackup || len(impact.Changes) != 1 || impact.Changes[0].Kind != "additive" {
		t.Fatalf("safe impact = %+v", impact)
	}
}
