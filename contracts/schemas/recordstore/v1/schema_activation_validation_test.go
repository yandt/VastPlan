package recordstorev1

import (
	"strings"
	"testing"
)

func TestValidateSchemaActivationRequiresApprovalForSignedMigration(t *testing.T) {
	activation := &SchemaActivation{CandidateID: "candidate-1", PlanDigest: strings.Repeat("a", 64), Mode: SchemaActivationAutomatic,
		Models: []SchemaMigrationAuthorization{{Model: ModelRef{ID: "example.order", SchemaVersion: 2, SHA256: strings.Repeat("b", 64)}, Kind: "signed", MigrationID: "example.order.v2", AllowSigned: true, BackupRef: "backup://one"}}}
	if err := ValidateSchemaActivation(activation); err == nil {
		t.Fatal("automatic mode must not authorize signed migration")
	}
	activation.Mode, activation.ApprovedBy = SchemaActivationApproved, "approver"
	if err := ValidateSchemaActivation(activation); err != nil {
		t.Fatalf("approved signed migration rejected: %v", err)
	}
}
