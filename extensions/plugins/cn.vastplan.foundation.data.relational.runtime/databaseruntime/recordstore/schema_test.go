package recordstore

import (
	"strings"
	"testing"

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
)

func TestSchemaPlanAllowsOnlySafeAdditiveChanges(t *testing.T) {
	for _, provider := range []string{"postgresql", "mysql"} {
		dialect, _ := DialectFor(provider)
		created, err := PlanMigration(dialect, nil, testModel())
		if err != nil || created.Kind != MigrationCreate || len(created.Statements) != 1 {
			t.Fatalf("%s create plan: %+v %v", provider, created, err)
		}
		next := testModel()
		next.SchemaVersion = 2
		next.Fields = append(next.Fields, datamodelv1.Field{ID: "note", Column: "note", Type: "string", MaxLength: 256, Nullable: true, Sensitivity: "internal"})
		next.Indexes = append(next.Indexes, datamodelv1.Index{ID: "byNote", Fields: []string{"note"}})
		additive, err := PlanMigration(dialect, ptrModel(testModel()), next)
		expectedStatements := 2
		if provider == "mysql" {
			expectedStatements = 1
		}
		if err != nil || additive.Kind != MigrationAdditive || len(additive.Statements) != expectedStatements {
			t.Fatalf("%s additive plan: %+v %v", provider, additive, err)
		}
		unsafe := testModel()
		unsafe.SchemaVersion = 3
		unsafe.Fields = append(unsafe.Fields, datamodelv1.Field{ID: "note", Column: "note", Type: "string", MaxLength: 256, Nullable: true, Sensitivity: "internal"})
		unsafe.Indexes = append(unsafe.Indexes, datamodelv1.Index{ID: "byNote", Fields: []string{"note"}})
		unsafe.Fields[2].Nullable = true
		manual, err := PlanMigration(dialect, &next, unsafe)
		if err != nil || manual.Kind != MigrationManual || len(manual.Reasons) == 0 {
			t.Fatalf("%s unsafe plan: %+v %v", provider, manual, err)
		}
	}
}

func TestDDLUsesProviderDialectAndInternalLedgers(t *testing.T) {
	postgres, _ := DialectFor("postgresql")
	created, _ := PlanMigration(postgres, nil, testModel())
	if !strings.Contains(created.Statements[0].SQL, `"orders"`) || !strings.Contains(created.Statements[0].SQL, "JSON") && strings.Contains(created.Statements[0].SQL, "payload") {
		t.Fatal(created.Statements[0].SQL)
	}
	mysql, _ := DialectFor("mysql")
	statements := InternalSchemaStatements(mysql)
	if len(statements) != 3 || !strings.Contains(statements[0].SQL, "`vastplan_schema_migrations`") {
		t.Fatalf("MySQL internal schema: %+v", statements)
	}
	if !strings.Contains(statements[1].SQL, "PRIMARY KEY (`identity_hash`)") ||
		!strings.Contains(statements[2].SQL, "UNIQUE KEY `vastplan_outbox_idempotency` (`identity_hash`)") {
		t.Fatalf("MySQL 内部幂等键必须使用定长身份摘要，不能产生超宽复合索引: %+v", statements)
	}
}

func TestIdentityDigestIsUnambiguousAndStable(t *testing.T) {
	first := identityDigest("ab", "c")
	if first == identityDigest("a", "bc") || first != identityDigest("ab", "c") || len(first) != 64 {
		t.Fatalf("幂等身份摘要必须按字段边界稳定生成: %q", first)
	}
}

func ptrModel(value datamodelv1.Model) *datamodelv1.Model { return &value }
