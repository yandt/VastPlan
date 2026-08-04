package recordstore

import (
	"strings"
	"testing"
	"time"

	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

func TestCompilerBuildsScopedParameterizedCRUD(t *testing.T) {
	now := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	scope := TrustedScope{TenantID: "tenant-a", ServiceID: "orders", ActorID: "plugin-orders"}
	for _, provider := range []string{"postgresql", "mysql"} {
		t.Run(provider, func(t *testing.T) {
			dialect, err := DialectFor(provider)
			if err != nil {
				t.Fatal(err)
			}
			compiler, err := NewCompiler(dialect, testModel())
			if err != nil {
				t.Fatal(err)
			}
			create, prepared, err := compiler.Create(recordstorev1.Record{"id": raw("f119df99-6c60-4e21-9c44-47766593c8e2"), "name": raw("first")}, scope, now)
			if err != nil {
				t.Fatal(err)
			}
			if string(prepared["tenantId"]) != `"tenant-a"` || string(prepared["revision"]) != `"1"` {
				t.Fatalf("可信 scope/revision 未注入: %+v", prepared)
			}
			if strings.Contains(create.SQL, "tenant-a") || len(create.Parameters) != 7 {
				t.Fatalf("Create 必须全部参数化: %+v", create)
			}
			get, err := compiler.Get(recordstorev1.Key{"id": raw("f119df99-6c60-4e21-9c44-47766593c8e2")}, scope)
			if err != nil || !strings.Contains(get.SQL, "deleted_at") || len(get.Parameters) != 2 {
				t.Fatalf("Get scope/软删除编译错误: %+v %v", get, err)
			}
			update, err := compiler.Update(recordstorev1.UpdateRequest{Key: recordstorev1.Key{"id": raw("f119df99-6c60-4e21-9c44-47766593c8e2")}, Values: recordstorev1.Record{"name": raw("next")}, ExpectedRevision: 1}, scope, now)
			if err != nil || !strings.Contains(update.SQL, "revision") || len(update.Parameters) != 5 {
				t.Fatalf("Update CAS 编译错误: %+v %v", update, err)
			}
			remove, err := compiler.Delete(recordstorev1.DeleteRequest{Key: recordstorev1.Key{"id": raw("f119df99-6c60-4e21-9c44-47766593c8e2")}, ExpectedRevision: 2}, scope, now)
			if err != nil || !strings.HasPrefix(remove.SQL, "UPDATE") || len(remove.Parameters) != 4 {
				t.Fatalf("Soft delete 编译错误: %+v %v", remove, err)
			}
			if provider == "postgresql" && !strings.Contains(create.SQL, "$1") {
				t.Fatal("PostgreSQL 应使用编号占位符")
			}
			if provider == "mysql" && strings.Contains(create.SQL, "$1") {
				t.Fatal("MySQL 应使用问号占位符")
			}
		})
	}
}

func TestCompilerListWhitelistsFieldsAndCursor(t *testing.T) {
	dialect, _ := DialectFor("postgresql")
	compiler, _ := NewCompiler(dialect, testModel())
	ref := recordstorev1.ModelRef{ID: "example.order", SchemaVersion: 1, SHA256: strings.Repeat("a", 64)}
	cursor := EncodeCursor(ref, 20)
	statement, offset, err := compiler.List(recordstorev1.ListRequest{Model: ref, Filters: []recordstorev1.Filter{{Field: "name", Operator: "prefix", Value: raw("abc")}}, Sort: []recordstorev1.Sort{{Field: "name", Direction: "desc"}}, Limit: 10, Cursor: cursor}, TrustedScope{TenantID: "tenant-a"})
	if err != nil || offset != 20 || !strings.Contains(statement.SQL, "ORDER BY \"name\" DESC, \"id\" ASC") {
		t.Fatalf("List 编译失败: %s offset=%d err=%v", statement.SQL, offset, err)
	}
	if strings.Contains(statement.SQL, "abc") {
		t.Fatal("Filter 值不得拼入 SQL")
	}
	if _, _, err := compiler.List(recordstorev1.ListRequest{Model: ref, Sort: []recordstorev1.Sort{{Field: "missing", Direction: "asc"}}, Limit: 10}, TrustedScope{TenantID: "tenant-a"}); err == nil {
		t.Fatal("未知排序字段必须拒绝")
	}
	other := ref
	other.SHA256 = strings.Repeat("b", 64)
	if _, _, err := compiler.List(recordstorev1.ListRequest{Model: other, Limit: 10, Cursor: cursor}, TrustedScope{TenantID: "tenant-a"}); err == nil {
		t.Fatal("跨模型 cursor 必须拒绝")
	}
}

func TestCompilerRejectsScopeAndTypeForgery(t *testing.T) {
	dialect, _ := DialectFor("postgresql")
	compiler, _ := NewCompiler(dialect, testModel())
	_, _, err := compiler.Create(recordstorev1.Record{"id": raw("id"), "tenantId": raw("other"), "name": raw("first")}, TrustedScope{TenantID: "tenant-a"}, time.Now())
	if err == nil {
		t.Fatal("调用方不得伪造 tenant scope")
	}
	_, _, err = compiler.Create(recordstorev1.Record{"id": raw("id"), "name": raw("first"), "revision": raw(1)}, TrustedScope{TenantID: "tenant-a"}, time.Now())
	if err == nil {
		t.Fatal("int64 wire 值必须使用字符串")
	}
}
