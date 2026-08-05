package nodeagent

import (
	"strings"
	"testing"

	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

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

func TestSchemaActivationFailureHasDedicatedRuntimeStage(t *testing.T) {
	err := &SchemaActivationError{ModelID: "example.order", Phase: "verify", Err: assertError("drift")}
	if got := runtimeFailureStage(err); got != "schema_verify" {
		t.Fatalf("runtime failure stage = %s", got)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
