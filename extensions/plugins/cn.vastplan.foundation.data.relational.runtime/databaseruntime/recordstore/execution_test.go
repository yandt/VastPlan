package recordstore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

type engineSession struct {
	ref         recordstorev1.ModelRef
	idempotency map[string][2]databasev1.Value
	mutations   int
	row         []databasev1.Value
}

func (s *engineSession) Query(_ context.Context, statement databasev1.Statement, _ int) (databasev1.QueryResult, error) {
	switch {
	case strings.Contains(statement.SQL, "vastplan_schema_migrations"):
		return databasev1.QueryResult{Rows: [][]databasev1.Value{{
			{Type: "int64", Value: mustJSON("1")}, {Type: "string", Value: mustJSON(s.ref.SHA256)},
		}}}, nil
	case strings.Contains(statement.SQL, "vastplan_record_idempotency"):
		key := valueString(statement.Parameters[len(statement.Parameters)-1])
		if existing, ok := s.idempotency[key]; ok {
			return databasev1.QueryResult{Rows: [][]databasev1.Value{{existing[0], existing[1]}}}, nil
		}
		return databasev1.QueryResult{}, nil
	default:
		if s.row == nil {
			return databasev1.QueryResult{}, nil
		}
		return databasev1.QueryResult{Rows: [][]databasev1.Value{s.row}}, nil
	}
}

func (s *engineSession) Execute(_ context.Context, statement databasev1.Statement) (databasev1.ExecuteResult, error) {
	if strings.Contains(statement.SQL, "vastplan_record_idempotency") {
		key := valueString(statement.Parameters[5])
		s.idempotency[key] = [2]databasev1.Value{statement.Parameters[6], statement.Parameters[7]}
		return databasev1.ExecuteResult{RowsAffected: 1}, nil
	}
	s.mutations++
	return databasev1.ExecuteResult{RowsAffected: 1}, nil
}

func TestEngineCreateIsIdempotentAndScopeBound(t *testing.T) {
	model := testModel()
	document, _ := MarshalModel(model)
	ref, _ := ModelRef(document)
	entry := ModelEntry{OwnerPluginID: "cn.vastplan.example.orders", Ref: ref, Model: model}
	dialect, _ := DialectFor("postgresql")
	compiler, _ := NewCompiler(dialect, model)
	session := &engineSession{ref: ref, idempotency: map[string][2]databasev1.Value{}}
	engine := NewEngine()
	request := recordstorev1.CreateRequest{Model: ref, Record: recordstorev1.Record{"id": raw("f119df99-6c60-4e21-9c44-47766593c8e2"), "name": raw("first")}, IdempotencyKey: "create:order-1"}
	scope := TrustedScope{TenantID: "tenant-a", ServiceID: "orders", ActorID: "orders"}
	identity := ExecutionIdentity{OwnerPluginID: entry.OwnerPluginID, ModelID: model.ID, TenantID: "tenant-a", ServiceID: "orders", CallerID: "orders"}
	first, err := engine.Create(context.Background(), session, compiler, entry, request, scope, identity)
	if err != nil || string(first.Record["tenantId"]) != `"tenant-a"` || session.mutations != 1 {
		t.Fatalf("首次 Create 失败: %+v mutations=%d err=%v", first, session.mutations, err)
	}
	second, err := engine.Create(context.Background(), session, compiler, entry, request, scope, identity)
	if err != nil || string(second.Record["name"]) != `"first"` || session.mutations != 1 {
		t.Fatalf("幂等重放不得重复写入: %+v mutations=%d err=%v", second, session.mutations, err)
	}
	request.Record["name"] = raw("changed")
	if _, err := engine.Create(context.Background(), session, compiler, entry, request, scope, identity); err == nil {
		t.Fatal("同幂等键不同请求必须冲突")
	}
}

func TestEngineGetAndListDecodeOnlyModelFields(t *testing.T) {
	model := testModel()
	document, _ := MarshalModel(model)
	ref, _ := ModelRef(document)
	entry := ModelEntry{OwnerPluginID: "cn.vastplan.example.orders", Ref: ref, Model: model}
	dialect, _ := DialectFor("mysql")
	compiler, _ := NewCompiler(dialect, model)
	row := []databasev1.Value{
		{Type: "string", Value: mustJSON("f119df99-6c60-4e21-9c44-47766593c8e2")},
		{Type: "string", Value: mustJSON("tenant-a")}, {Type: "string", Value: mustJSON("first")},
		{Type: "int64", Value: mustJSON("1")}, {Type: "timestamp", Value: mustJSON("2026-08-05T04:00:00Z")},
		{Type: "timestamp", Value: mustJSON("2026-08-05T04:00:00Z")}, {Type: "null"},
	}
	session := &engineSession{ref: ref, idempotency: map[string][2]databasev1.Value{}, row: row}
	engine := NewEngine()
	got, err := engine.Get(context.Background(), session, compiler, entry, recordstorev1.GetRequest{Model: ref, Key: recordstorev1.Key{"id": raw("f119df99-6c60-4e21-9c44-47766593c8e2")}}, TrustedScope{TenantID: "tenant-a"})
	if err != nil || len(got.Record) != len(model.Fields) || string(got.Record["revision"]) != `"1"` {
		t.Fatalf("Get 解码错误: %+v err=%v", got, err)
	}
}

func valueString(value databasev1.Value) string {
	var result string
	_ = json.Unmarshal(value.Value, &result)
	return result
}
