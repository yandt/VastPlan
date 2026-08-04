package datamigrationv1

import (
	"strings"
	"testing"
)

func TestParseAcceptsGovernedProviderPlans(t *testing.T) {
	raw := []byte(`{"contract":"data.migration.v1","id":"example.order.v2","modelId":"example.order","from":{"schemaVersion":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"to":{"schemaVersion":2,"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"requiresBackup":true,"requiresApproval":true,"retrySafe":true,"providers":[{"providerId":"postgresql","statements":["ALTER TABLE orders DROP COLUMN legacy"]}]}`)
	migration, err := Parse(raw)
	if err != nil || migration.ID != "example.order.v2" {
		t.Fatalf("解析迁移失败: %+v %v", migration, err)
	}
	if plan, ok := migration.Plan("postgresql"); !ok || len(plan.Statements) != 1 {
		t.Fatalf("Provider 计划缺失: %+v", plan)
	}
}

func TestParseRejectsUngovernedOrMultiStatementSQL(t *testing.T) {
	valid := `{"contract":"data.migration.v1","id":"example.order.v2","modelId":"example.order","from":{"schemaVersion":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"to":{"schemaVersion":2,"sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"requiresBackup":true,"requiresApproval":true,"retrySafe":true,"providers":[{"providerId":"postgresql","statements":["ALTER TABLE orders DROP COLUMN legacy"]}]}`
	for _, raw := range []string{
		strings.Replace(valid, `"requiresBackup":true`, `"requiresBackup":false`, 1),
		strings.Replace(valid, `ALTER TABLE orders DROP COLUMN legacy`, `ALTER TABLE orders DROP COLUMN legacy; DROP TABLE orders`, 1),
		strings.Replace(valid, `ALTER TABLE orders DROP COLUMN legacy`, `DELETE FROM vastplan_schema_migrations`, 1),
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("危险迁移必须拒绝: %s", raw)
		}
	}
}
