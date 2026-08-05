package deploymentmanager

import (
	"strings"
	"testing"

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

func TestSchemaActivationForRevisionBindsApprovedSafePlan(t *testing.T) {
	oldCatalog, targetCatalog := migrationCatalogs(false)
	impact := plugininstallation.BuildSchemaImpact(oldCatalog, targetCatalog)
	state := &tenantState{Revisions: []platformadminapi.ServiceRevision{
		{ID: 1, Deployment: "services", Active: true, DataModelCatalog: oldCatalog},
		{ID: 2, Deployment: "services", ApprovedBy: "bob", DataModelCatalog: targetCatalog},
	}, InstallationCandidates: map[string]installationCandidateRecord{"installation-1": {ServiceRevisionID: 2, Preview: plugininstallation.Preview{Impact: plugininstallation.Impact{Schema: impact}}, Migration: initialMigrationState(impact, "2026-08-05T00:00:00Z")}}}
	authorization, err := schemaActivationForRevision(state, state.Revisions[1])
	if err != nil || authorization == nil || authorization.PlanDigest != impact.Digest || len(authorization.Models) != 1 || !authorization.Models[0].AllowSafe {
		t.Fatalf("schema authorization = %+v err=%v", authorization, err)
	}
}

func TestSchemaActivationForRevisionRequiresBackupForSignedPlan(t *testing.T) {
	oldCatalog, targetCatalog := migrationCatalogs(true)
	impact := plugininstallation.BuildSchemaImpact(oldCatalog, targetCatalog)
	record := installationCandidateRecord{ServiceRevisionID: 2, Preview: plugininstallation.Preview{Impact: plugininstallation.Impact{Schema: impact}}, Migration: initialMigrationState(impact, "2026-08-05T00:00:00Z")}
	state := &tenantState{Revisions: []platformadminapi.ServiceRevision{{ID: 1, Deployment: "services", Active: true, DataModelCatalog: oldCatalog}, {ID: 2, Deployment: "services", ApprovedBy: "bob", DataModelCatalog: targetCatalog}}, InstallationCandidates: map[string]installationCandidateRecord{"installation-1": record}}
	if _, err := schemaActivationForRevision(state, state.Revisions[1]); err == nil {
		t.Fatal("signed migration without backup must fail closed")
	}
	record.Migration.BackupRef = "backup://orders/v1"
	state.InstallationCandidates["installation-1"] = record
	authorization, err := schemaActivationForRevision(state, state.Revisions[1])
	if err != nil || len(authorization.Models) != 1 || !authorization.Models[0].AllowSigned {
		t.Fatalf("signed authorization = %+v err=%v", authorization, err)
	}
}

func migrationCatalogs(signed bool) (deploymentpublication.DataModelCatalog, deploymentpublication.DataModelCatalog) {
	base := datamodelv1.Model{Contract: "data.model.v1", ID: "example.order", SchemaVersion: 1, Storage: datamodelv1.StorageBinding{Kind: "platform-control", Table: "orders"}, Fields: []datamodelv1.Field{{ID: "id", Column: "id", Type: "uuid", Sensitivity: "internal"}}, PrimaryKey: []string{"id"}, Indexes: []datamodelv1.Index{}, UniqueConstraints: []datamodelv1.UniqueConstraint{}, Scope: datamodelv1.Scope{Tenant: "none", Service: "none"}, Deletion: datamodelv1.DeletionPolicy{Mode: "hard"}}
	next := base
	next.SchemaVersion = 2
	next.Fields = append([]datamodelv1.Field(nil), base.Fields...)
	if signed {
		next.Fields[0].Type = "string"
	} else {
		next.Fields = append(next.Fields, datamodelv1.Field{ID: "note", Column: "note", Type: "string", Nullable: true, Sensitivity: "internal"})
	}
	oldRef, nextRef := recordstorev1.ModelRef{ID: base.ID, SchemaVersion: 1, SHA256: strings.Repeat("a", 64)}, recordstorev1.ModelRef{ID: next.ID, SchemaVersion: 2, SHA256: strings.Repeat("b", 64)}
	oldCatalog := deploymentpublication.DataModelCatalog{Digest: strings.Repeat("c", 64), Models: []deploymentpublication.DataModelDescriptor{{OwnerPluginID: "cn.example", Ref: oldRef, Model: base}}}
	target := deploymentpublication.DataModelCatalog{Digest: strings.Repeat("d", 64), Models: []deploymentpublication.DataModelDescriptor{{OwnerPluginID: "cn.example", Ref: nextRef, Model: next}}}
	if signed {
		target.Migrations = []deploymentpublication.DataMigrationDescriptor{{OwnerPluginID: "cn.example", Ref: recordstorev1.MigrationRef{ID: "example.order.v2", ModelID: base.ID, FromVersion: 1, ToVersion: 2, SHA256: strings.Repeat("e", 64)}}}
	}
	return oldCatalog, target
}
