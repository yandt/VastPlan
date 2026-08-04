package databaseruntime

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/sqlsharedstate"
)

func TestPostgreSQLProviderIntegration(t *testing.T) {
	runProviderIntegration(t, "postgresql", "VASTPLAN_TEST_POSTGRESQL", "select $1::bigint as value")
}

func TestMySQLProviderIntegration(t *testing.T) {
	runProviderIntegration(t, "mysql", "VASTPLAN_TEST_MYSQL", "select cast(? as signed) as value")
}

func runProviderIntegration(t *testing.T, providerID, prefix, query string) {
	t.Helper()
	endpoint, user, password := os.Getenv(prefix+"_ENDPOINT"), os.Getenv(prefix+"_USER"), os.Getenv(prefix+"_PASSWORD")
	if endpoint == "" || user == "" || password == "" {
		t.Skipf("未配置 %s_ENDPOINT/USER/PASSWORD，跳过真实数据库集成测试", prefix)
	}
	tlsMode := os.Getenv(prefix + "_TLS_MODE")
	if tlsMode == "" {
		tlsMode = "verify-full"
	}
	options, err := json.Marshal(map[string]any{"user": user, "tlsMode": tlsMode, "serverName": os.Getenv(prefix + "_SERVER_NAME")})
	if err != nil {
		t.Fatal(err)
	}
	spec := providerSpec(providerID, endpoint)
	spec.Database = os.Getenv(prefix + "_DATABASE")
	spec.Options = options
	spec.Pool.MinIdle, spec.Pool.MaxIdle, spec.Pool.MaxOpen = 1, 2, 4
	policy := ProviderSecurityPolicy{AllowInsecureTLS: tlsMode == "disable"}
	registry := NewRegistry()
	var provider Provider
	if providerID == "postgresql" {
		provider = NewPostgreSQLProvider(policy)
	} else {
		provider = NewMySQLProvider(policy)
	}
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	pool, err := registry.OpenPool(context.Background(), spec, &testMaterialSource{value: []byte(password)})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := pool.Probe(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := pool.Query(ctx, databasev1.Statement{SQL: query, Parameters: []databasev1.Value{{
		Type: "int64", Value: json.RawMessage(`"7"`),
	}}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 1 || result.Rows[0][0].Type != "int64" {
		t.Fatalf("真实 Provider 返回值不符合契约: %+v", result)
	}
	runDeclarativeDataIntegration(t, providerID, pool)
}

type integrationSessions struct{ pool Pool }

func (s integrationSessions) Read(ctx context.Context, work func(recordstore.Session) error) error {
	return work(s.pool)
}

func (s integrationSessions) Write(ctx context.Context, work func(recordstore.Session) error) error {
	transaction, err := s.pool.Begin(ctx, databasev1.TransactionOptions{Isolation: "serializable", TimeoutMS: 30_000})
	if err != nil {
		return err
	}
	if err := work(transaction); err != nil {
		_ = transaction.Rollback(context.Background())
		return err
	}
	return transaction.Commit(ctx)
}

func runDeclarativeDataIntegration(t *testing.T, providerID string, pool Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dialect, err := recordstore.DialectFor(providerID)
	if err != nil {
		t.Fatal(err)
	}
	model := datamodelv1.Model{
		Contract: "data.model.v1", ID: "integration.record." + providerID, SchemaVersion: 1,
		Storage: datamodelv1.StorageBinding{Kind: "connection-ref", Table: "vastplan_p3_record_" + providerID},
		Fields: []datamodelv1.Field{
			{ID: "id", Column: "id", Type: "uuid", Sensitivity: "internal"},
			{ID: "tenantId", Column: "tenant_id", Type: "string", Sensitivity: "confidential", MaxLength: 160},
			{ID: "name", Column: "name", Type: "string", Sensitivity: "internal", MaxLength: 160},
			{ID: "revision", Column: "revision", Type: "int64", Sensitivity: "internal"},
			{ID: "createdAt", Column: "created_at", Type: "timestamp", Sensitivity: "internal"},
			{ID: "updatedAt", Column: "updated_at", Type: "timestamp", Sensitivity: "internal"},
		},
		PrimaryKey: []string{"id"}, Indexes: []datamodelv1.Index{{ID: "byName", Fields: []string{"name"}}},
		UniqueConstraints: []datamodelv1.UniqueConstraint{{ID: "tenantName", Fields: []string{"tenantId", "name"}}},
		Scope:             datamodelv1.Scope{Tenant: "required", Service: "none"}, OptimisticLock: &datamodelv1.OptimisticLock{Field: "revision"},
		Audit: &datamodelv1.AuditFields{CreatedAt: "createdAt", UpdatedAt: "updatedAt"}, Deletion: datamodelv1.DeletionPolicy{Mode: "hard"},
	}
	document, _ := recordstore.MarshalModel(model)
	ref, err := recordstore.ModelRef(document)
	if err != nil {
		t.Fatal(err)
	}
	entry := recordstore.ModelEntry{OwnerPluginID: "cn.vastplan.integration", Ref: ref, Model: model}
	pinned, ok := pool.(PinnedPool)
	if !ok {
		t.Fatal("真实第一方 Provider 必须支持 pinned Schema session")
	}
	if err := pinned.WithPinned(ctx, func(session PinnedSession) error {
		schemaService := &Service{recordModels: recordstore.NewCatalog()}
		_, applyErr := schemaService.applySchema(ctx, session, dialect, entry, "", nil)
		return applyErr
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Execute(context.Background(), databasev1.Statement{SQL: "DROP TABLE IF EXISTS " + dialect.Quote(model.Storage.Table), Parameters: []databasev1.Value{}})
	})
	compiler, _ := recordstore.NewCompiler(dialect, model)
	engine := recordstore.NewEngine()
	transaction, err := pool.Begin(ctx, databasev1.TransactionOptions{Isolation: "serializable", TimeoutMS: 30_000})
	if err != nil {
		t.Fatal(err)
	}
	request := recordstorev1.CreateRequest{Model: ref, Record: recordstorev1.Record{
		"id": json.RawMessage(`"f119df99-6c60-4e21-9c44-47766593c8e2"`), "name": json.RawMessage(`"integration"`),
	}, IdempotencyKey: "integration:create:" + providerID}
	identity := recordstore.ExecutionIdentity{OwnerPluginID: entry.OwnerPluginID, ModelID: model.ID, TenantID: "tenant-integration", CallerID: "cn.vastplan.integration"}
	created, err := engine.Create(ctx, transaction, compiler, entry, request, recordstore.TrustedScope{TenantID: "tenant-integration", ActorID: "cn.vastplan.integration"}, identity)
	if err != nil {
		_ = transaction.Rollback(context.Background())
		t.Fatal(err)
	}
	if err := transaction.Commit(ctx); err != nil || string(created.Record["revision"]) != `"1"` {
		t.Fatalf("真实 Record Store Create 失败: %+v %v", created, err)
	}
	stateStore, err := sqlsharedstate.NewStore(dialect, integrationSessions{pool: pool})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range sqlsharedstate.SchemaStatements(dialect) {
		if _, err := pool.Execute(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	scope := sharedstate.Scope{Kind: sharedstate.ScopeTenant, TenantID: "tenant-integration", PluginID: "cn.vastplan.integration", RuntimeScope: "p3", Namespace: providerID}
	createdState, err := stateStore.Create(ctx, scope, "active", []byte(`{"ready":true}`))
	if err != nil || createdState.Revision != 1 {
		t.Fatalf("真实 SQL Shared State Create 失败: %+v %v", createdState, err)
	}
	if _, err := stateStore.Update(ctx, scope, "active", []byte(`{"ready":false}`), 1); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.Delete(ctx, scope, "active", 2); err != nil {
		t.Fatal(err)
	}
}
