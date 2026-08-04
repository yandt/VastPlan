package datamodelv1

import (
	"strings"
	"testing"
)

func TestParseValidatesDataShapeWithoutBusinessActions(t *testing.T) {
	raw := []byte(`{
  "contract":"data.model.v1","id":"platform.database.connection","schemaVersion":1,
  "storage":{"kind":"platform-control","table":"database_connections"},
  "fields":[
    {"id":"id","column":"id","type":"uuid","nullable":false,"sensitivity":"internal"},
    {"id":"tenantId","column":"tenant_id","type":"string","nullable":false,"sensitivity":"confidential"},
    {"id":"revision","column":"revision","type":"int64","nullable":false,"sensitivity":"internal"},
    {"id":"createdAt","column":"created_at","type":"timestamp","nullable":false,"sensitivity":"internal"},
    {"id":"updatedAt","column":"updated_at","type":"timestamp","nullable":false,"sensitivity":"internal"}
  ],
  "primaryKey":["id"],"indexes":[{"id":"byTenant","fields":["tenantId"],"unique":false}],
  "uniqueConstraints":[{"id":"tenantIdentity","fields":["tenantId","id"]}],
  "scope":{"tenant":"required","service":"none"},"optimisticLock":{"field":"revision"},
  "audit":{"createdAt":"createdAt","updatedAt":"updatedAt"},"deletion":{"mode":"hard"}
}`)
	model, err := Parse(raw)
	if err != nil || model.ID != "platform.database.connection" {
		t.Fatalf("合法 DataModel 应通过: model=%+v err=%v", model, err)
	}
	for _, forbidden := range []string{"actions", "permissions", "routes", "workbench"} {
		bad := raw[:len(raw)-2]
		bad = append(bad, []byte(`,"`+forbidden+`":{}}`)...)
		if _, err := Parse(bad); err == nil {
			t.Fatalf("DataModel 不得声明业务字段 %s", forbidden)
		}
	}
}

func TestParseRejectsUnsafeReferences(t *testing.T) {
	raw := `{"contract":"data.model.v1","id":"demo","schemaVersion":1,"storage":{"kind":"connection-ref","table":"demo"},"fields":[{"id":"id","column":"id","type":"string","nullable":true,"sensitivity":"public"}],"primaryKey":["id"],"indexes":[],"uniqueConstraints":[],"scope":{"tenant":"none","service":"none"},"deletion":{"mode":"hard"}}`
	if _, err := Parse([]byte(raw)); err == nil || !strings.Contains(err.Error(), "主键") {
		t.Fatalf("可空主键必须被拒绝: %v", err)
	}
}
