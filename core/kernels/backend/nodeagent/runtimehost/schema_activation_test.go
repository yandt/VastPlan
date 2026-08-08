package runtimehost

import (
	"strings"
	"testing"

	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

func TestSchemaActivationIsSkippedWithoutExplicitAuthorization(t *testing.T) {
	transaction := applyTransaction{modelInventory: &recordstorev1.SyncModelsRequest{
		Models: []recordstorev1.SignedModel{{Model: recordstorev1.ModelRef{ID: "platform.database.connection"}}},
	}}
	if err := transaction.applyTrustedSchemaActivation(t.Context()); err != nil {
		t.Fatalf("仅同步 Inventory 时不得在平台数据库配置前执行 Schema Plan: %v", err)
	}
}

func TestSchemaEvidenceSeparatesSafeAndSignedAuthorization(t *testing.T) {
	model := recordstorev1.ModelRef{ID: "example.order", SchemaVersion: 2, SHA256: strings.Repeat("a", 64)}
	safe, err := schemaEvidence(recordstorev1.SchemaMigrationAuthorization{Model: model, Kind: "additive", AllowSafe: true})
	if err != nil || len(safe) != 1 || safe[0] != "database.schema-controller/platform.control" {
		t.Fatalf("safe evidence = %v err=%v", safe, err)
	}
	signed := recordstorev1.SchemaMigrationAuthorization{Model: model, Kind: "signed", MigrationID: "example.order.v2", AllowSigned: true, BackupRef: "backup://orders/v1"}
	evidence, err := schemaEvidence(signed)
	if err != nil || len(evidence) != 3 {
		t.Fatalf("signed evidence = %v err=%v", evidence, err)
	}
	signed.BackupRef = ""
	if _, err := schemaEvidence(signed); err == nil {
		t.Fatal("signed migration without backup evidence must fail closed")
	}
}
