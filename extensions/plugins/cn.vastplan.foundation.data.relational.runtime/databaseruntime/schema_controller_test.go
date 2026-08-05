package databaseruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

type schemaSession struct {
	state      *recordstore.SchemaState
	statements []string
}

func (s *schemaSession) Begin(context.Context, databasev1.TransactionOptions) (Transaction, error) {
	return s, nil
}
func (*schemaSession) Commit(context.Context) error   { return nil }
func (*schemaSession) Rollback(context.Context) error { return nil }

type schemaPlatformStore struct {
	*schemaSession
	closed chan struct{}
}

func (*schemaPlatformStore) ProviderID() string { return "postgresql" }
func (s *schemaPlatformStore) Read(ctx context.Context, work func(recordstore.Session) error) error {
	return work(s.schemaSession)
}
func (s *schemaPlatformStore) Write(ctx context.Context, work func(recordstore.Session) error) error {
	return work(s.schemaSession)
}
func (*schemaPlatformStore) Begin(context.Context, databasev1.TransactionOptions) (Transaction, error) {
	return nil, errors.New("not used")
}
func (s *schemaPlatformStore) WithPinned(ctx context.Context, work func(PinnedSession) error) error {
	return work(s.schemaSession)
}
func (s *schemaPlatformStore) Closed() <-chan struct{} { return s.closed }

func TestSchemaControllerRequiresSignedMigrationBackupAndApproval(t *testing.T) {
	previous := schemaTestModel()
	previousDocument, _ := recordstore.MarshalModel(previous)
	previousRef, _ := recordstore.ModelRef(previousDocument)
	target := previous
	target.SchemaVersion = 2
	target.Fields = append(target.Fields, datamodelv1.Field{ID: "requiredName", Column: "required_name", Type: "string", MaxLength: 160, Sensitivity: "internal"})
	targetDocument, _ := recordstore.MarshalModel(target)
	targetRef, _ := recordstore.ModelRef(targetDocument)
	migrationDocument := []byte(fmt.Sprintf(`{"contract":"data.migration.v1","id":"example.order.v2","modelId":"example.order","from":{"schemaVersion":1,"sha256":"%s"},"to":{"schemaVersion":2,"sha256":"%s"},"requiresBackup":true,"requiresApproval":true,"retrySafe":true,"providers":[{"providerId":"postgresql","statements":["ALTER TABLE orders ADD COLUMN IF NOT EXISTS required_name TEXT NOT NULL"]}]}`, previousRef.SHA256, targetRef.SHA256))
	migrationRef := recordstorev1.MigrationRef{ID: "example.order.v2", ModelID: target.ID, FromVersion: 1, ToVersion: 2, SHA256: fmt.Sprintf("%x", sha256.Sum256(migrationDocument))}
	service := &Service{recordModels: recordstore.NewCatalog()}
	_, err := service.recordModels.Replace(recordstorev1.SyncModelsRequest{Generation: 1,
		InventoryDigest: "1111111111111111111111111111111111111111111111111111111111111111",
		Models:          []recordstorev1.SignedModel{recordstore.EncodeSignedModel("cn.vastplan.application.orders", "2222222222222222222222222222222222222222222222222222222222222222", targetRef, targetDocument)},
		Migrations:      []recordstorev1.SignedMigration{recordstore.EncodeSignedMigration("cn.vastplan.application.orders", "2222222222222222222222222222222222222222222222222222222222222222", migrationRef, migrationDocument)},
	})
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := service.recordModels.Resolve(targetRef)
	dialect, _ := recordstore.DialectFor("postgresql")
	session := &schemaSession{state: &recordstore.SchemaState{Version: 1, SHA256: previousRef.SHA256, Document: previous}}
	denied, err := service.applySchemaSession(context.Background(), session, dialect, entry, true, migrationRef.ID, func(string) bool { return false })
	if denied.Kind != recordstore.MigrationSigned || !errors.Is(err, recordstore.ErrStorageDenied) {
		t.Fatalf("缺少备份或审批证据必须拒绝: %+v %v", denied, err)
	}
	applied, err := service.applySchemaSession(context.Background(), session, dialect, entry, true, migrationRef.ID, func(string) bool { return true })
	if err != nil || applied.Kind != recordstore.MigrationSigned || session.state == nil || session.state.SHA256 != targetRef.SHA256 {
		t.Fatalf("签名迁移执行失败: %+v %+v %v", applied, session.state, err)
	}
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
			{Type: "string", Value: jsonString(s.state.MigrationID)}, {Type: "json", Value: document},
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
		var migrationID string
		if statement.Parameters[3].Type == "string" {
			_ = json.Unmarshal(statement.Parameters[3].Value, &migrationID)
		}
		var model datamodelv1.Model
		_ = json.Unmarshal(statement.Parameters[4].Value, &model)
		parsed := uint64(0)
		_, _ = fmt.Sscan(version, &parsed)
		s.state = &recordstore.SchemaState{Version: parsed, SHA256: digest, MigrationID: migrationID, Document: model}
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
	service := &Service{recordModels: recordstore.NewCatalog()}
	first, err := service.applySchemaSession(context.Background(), session, dialect, entry, true, "", nil)
	if err != nil || first.Kind != recordstore.MigrationCreate || session.state == nil || session.state.SHA256 != ref.SHA256 {
		t.Fatalf("首次 Schema apply 失败: plan=%+v state=%+v err=%v", first, session.state, err)
	}
	statementCount := len(session.statements)
	second, err := service.applySchemaSession(context.Background(), session, dialect, entry, true, "", nil)
	if err != nil || second.Kind != recordstore.MigrationNone {
		t.Fatalf("同一模型重复 apply 应幂等: %+v %v", second, err)
	}
	for _, statement := range session.statements[statementCount:] {
		if strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS \"orders\"") {
			t.Fatal("Ready 模型不得重复执行领域 DDL")
		}
	}
}

func TestPreparePlatformModelsCreatesSafeSchemaBeforeBinding(t *testing.T) {
	model := schemaTestModel()
	model.Storage = datamodelv1.StorageBinding{Kind: "platform-control", Table: "platform_orders"}
	document, _ := recordstore.MarshalModel(model)
	ref, _ := recordstore.ModelRef(document)
	service := &Service{recordModels: recordstore.NewCatalog()}
	_, err := service.recordModels.Replace(recordstorev1.SyncModelsRequest{Generation: 1,
		InventoryDigest: "1111111111111111111111111111111111111111111111111111111111111111",
		Models: []recordstorev1.SignedModel{recordstore.EncodeSignedModel("cn.vastplan.application.orders",
			"2222222222222222222222222222222222222222222222222222222222222222", ref, document)},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &schemaPlatformStore{schemaSession: &schemaSession{}, closed: make(chan struct{})}
	if err := service.PreparePlatformModels(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	if store.state == nil || store.state.SHA256 != ref.SHA256 {
		t.Fatalf("Platform DataModel 未写入迁移账本: %+v", store.state)
	}
	if joined := strings.Join(store.statements, "\n"); !strings.Contains(joined, `CREATE TABLE IF NOT EXISTS "platform_orders"`) {
		t.Fatalf("Platform DataModel 未执行安全建表: %s", joined)
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
	call.Credentials = []*contractv1.CredentialRef{
		{Name: "database.schema-backup/platform.control/example.order/example.order.v2", Scope: &scope},
		{Name: "database.schema-approval/platform.control/example.order/example.order.v2", Scope: &scope},
	}
	if !hasSignedMigrationEvidence(call, ref, "example.order", "example.order.v2") {
		t.Fatal("备份和审批证据齐全时应通过")
	}
	call.Credentials = call.Credentials[:1]
	if hasSignedMigrationEvidence(call, ref, "example.order", "example.order.v2") {
		t.Fatal("缺少任一治理证据必须拒绝")
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
