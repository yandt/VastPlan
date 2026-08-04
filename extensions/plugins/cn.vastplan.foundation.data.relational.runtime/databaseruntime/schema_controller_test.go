package databaseruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

type schemaSession struct {
	state      *recordstore.SchemaState
	statements []string
}

func (s *schemaSession) Query(_ context.Context, statement databasev1.Statement, _ int) (databasev1.QueryResult, error) {
	s.statements = append(s.statements, statement.SQL)
	if strings.Contains(statement.SQL, "pg_advisory") {
		return databasev1.QueryResult{Rows: [][]databasev1.Value{{{Type: "null"}}}}, nil
	}
	if strings.Contains(statement.SQL, "vastplan_schema_migrations") {
		if s.state == nil {
			return databasev1.QueryResult{}, nil
		}
		document, _ := json.Marshal(s.state.Document)
		return databasev1.QueryResult{Rows: [][]databasev1.Value{{
			{Type: "int64", Value: jsonString(fmt.Sprintf("%d", s.state.Version))}, {Type: "string", Value: jsonString(s.state.SHA256)},
			{Type: "json", Value: document},
		}}}, nil
	}
	return databasev1.QueryResult{}, nil
}

func (s *schemaSession) Execute(_ context.Context, statement databasev1.Statement) (databasev1.ExecuteResult, error) {
	s.statements = append(s.statements, statement.SQL)
	if strings.HasPrefix(statement.SQL, "INSERT INTO") && strings.Contains(statement.SQL, "vastplan_schema_migrations") {
		var version, digest string
		_ = json.Unmarshal(statement.Parameters[1].Value, &version)
		_ = json.Unmarshal(statement.Parameters[2].Value, &digest)
		var model datamodelv1.Model
		_ = json.Unmarshal(statement.Parameters[3].Value, &model)
		parsed := uint64(0)
		_, _ = fmt.Sscan(version, &parsed)
		s.state = &recordstore.SchemaState{Version: parsed, SHA256: digest, Document: model}
	}
	return databasev1.ExecuteResult{RowsAffected: 1}, nil
}

func TestSchemaControllerAppliesAndReplaysLedgerIdempotently(t *testing.T) {
	model := schemaTestModel()
	document, _ := recordstore.MarshalModel(model)
	ref, _ := recordstore.ModelRef(document)
	entry := recordstore.ModelEntry{OwnerPluginID: "cn.vastplan.application.orders", Ref: ref, Model: model}
	dialect, _ := recordstore.DialectFor("postgresql")
	session := &schemaSession{}
	first, err := applySchemaSession(context.Background(), session, dialect, entry, true)
	if err != nil || first.Kind != recordstore.MigrationCreate || session.state == nil || session.state.SHA256 != ref.SHA256 {
		t.Fatalf("首次 Schema apply 失败: plan=%+v state=%+v err=%v", first, session.state, err)
	}
	statementCount := len(session.statements)
	second, err := applySchemaSession(context.Background(), session, dialect, entry, true)
	if err != nil || second.Kind != recordstore.MigrationNone {
		t.Fatalf("同一模型重复 apply 应幂等: %+v %v", second, err)
	}
	for _, statement := range session.statements[statementCount:] {
		if strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS \"orders\"") {
			t.Fatal("Ready 模型不得重复执行领域 DDL")
		}
	}
}

func TestSchemaControllerRequiresSystemLeaderEvidence(t *testing.T) {
	ref := databasev1.ConnectionRef{ResourceID: "platform.control", Revision: 1}
	scope := "service"
	call := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "schema-controller"}, Credentials: []*contractv1.CredentialRef{{Name: "database.schema-controller/platform.control", Scope: &scope}}}
	if !hasSchemaControllerEvidence(call, ref) {
		t.Fatal("当前 Schema Controller leader evidence 应通过")
	}
	call.Credentials[0].Name = "database.schema-controller/other"
	if hasSchemaControllerEvidence(call, ref) {
		t.Fatal("跨连接 leader evidence 必须拒绝")
	}
}

func schemaTestModel() datamodelv1.Model {
	return datamodelv1.Model{
		Contract: "data.model.v1", ID: "example.order", SchemaVersion: 1,
		Storage:    datamodelv1.StorageBinding{Kind: "connection-ref", Table: "orders"},
		Fields:     []datamodelv1.Field{{ID: "id", Column: "id", Type: "uuid", Sensitivity: "internal"}},
		PrimaryKey: []string{"id"}, Indexes: []datamodelv1.Index{}, UniqueConstraints: []datamodelv1.UniqueConstraint{},
		Scope: datamodelv1.Scope{Tenant: "none", Service: "none"}, Deletion: datamodelv1.DeletionPolicy{Mode: "hard"},
	}
}

func jsonString(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }
