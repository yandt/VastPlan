package databaseruntime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

type testMaterial []byte

func (m testMaterial) Bytes() []byte { return m }

type testMaterialSource struct {
	value []byte
	calls int
}

func (s *testMaterialSource) WithMaterial(_ context.Context, use func(CredentialMaterial) error) error {
	s.calls++
	material := append([]byte(nil), s.value...)
	defer func() {
		for index := range material {
			material[index] = 0
		}
	}()
	return use(testMaterial(material))
}

type fakeProvider struct{ id string }

func (p fakeProvider) Descriptor() databasev1.ProviderDescriptor {
	return databasev1.ProviderDescriptor{
		ID: p.id, Version: "1.0.0", DisplayName: p.id,
		ConfigurationSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`),
		Capabilities:        databasev1.ProviderCapabilities{Query: true, Execute: true, Transactions: true},
	}
}

func (fakeProvider) Validate(_ context.Context, spec databasev1.ConnectionSpec) error {
	return databasev1.ValidateConnectionSpec(spec)
}

func (fakeProvider) OpenPool(_ context.Context, _ databasev1.ConnectionSpec, material MaterialSource) (Pool, error) {
	if err := material.WithMaterial(context.Background(), func(value CredentialMaterial) error {
		if string(value.Bytes()) != "test-password" {
			return errors.New("credential material 不匹配")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &fakePool{}, nil
}

type fakePool struct{ closed bool }

func (p *fakePool) Probe(context.Context) error {
	if p.closed {
		return errors.New("pool closed")
	}
	return nil
}

func (p *fakePool) Query(_ context.Context, _ databasev1.Statement, _ int) (databasev1.QueryResult, error) {
	if p.closed {
		return databasev1.QueryResult{}, errors.New("pool closed")
	}
	return testQueryResult(), nil
}

func (p *fakePool) Execute(_ context.Context, _ databasev1.Statement) (databasev1.ExecuteResult, error) {
	if p.closed {
		return databasev1.ExecuteResult{}, errors.New("pool closed")
	}
	return databasev1.ExecuteResult{RowsAffected: 1}, nil
}

func (p *fakePool) Begin(_ context.Context, _ databasev1.TransactionOptions) (Transaction, error) {
	if p.closed {
		return nil, errors.New("pool closed")
	}
	return &fakeTransaction{}, nil
}

func (p *fakePool) Stats() PoolStats {
	return PoolStats{Open: 2, Idle: 1, InUse: 1, MaxOpen: 8, Healthy: !p.closed}
}
func (p *fakePool) Close() error { p.closed = true; return nil }

type fakeTransaction struct{ ended bool }

func (t *fakeTransaction) Query(context.Context, databasev1.Statement, int) (databasev1.QueryResult, error) {
	if t.ended {
		return databasev1.QueryResult{}, NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("transaction ended"))
	}
	return testQueryResult(), nil
}
func (t *fakeTransaction) Execute(context.Context, databasev1.Statement) (databasev1.ExecuteResult, error) {
	if t.ended {
		return databasev1.ExecuteResult{}, NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("transaction ended"))
	}
	return databasev1.ExecuteResult{RowsAffected: 1}, nil
}
func (t *fakeTransaction) Commit(context.Context) error   { t.ended = true; return nil }
func (t *fakeTransaction) Rollback(context.Context) error { t.ended = true; return nil }

func testConnectionSpec(providerID string) databasev1.ConnectionSpec {
	return databasev1.ConnectionSpec{
		Ref:        databasev1.ConnectionRef{ResourceID: "orders.primary", Revision: 1},
		ProviderID: providerID, Endpoint: "db.internal:5432", Database: "orders", Options: json.RawMessage(`{}`),
		Credentials: commonv1.ManagedCredentialRef{
			Handle: "credential://managed/orders", Scope: "tenant", Owner: "cn.vastplan.platform.data.relational.connection-manager",
			Purpose: "database.connection", Version: 1,
		},
		Pool: databasev1.PoolPolicy{MaxIdle: 2, MaxOpen: 8, MaxLifetimeMS: 3600000, MaxIdleTimeMS: 300000, AcquireTimeoutMS: 5000, IdlePoolTTLMS: 600000},
	}
}

func testQueryResult() databasev1.QueryResult {
	return databasev1.QueryResult{
		Columns: []databasev1.Column{{Name: "id", DatabaseType: "bigint"}},
		Rows:    [][]databasev1.Value{{{Type: "int64", Value: json.RawMessage(`"1"`)}}},
	}
}

func TestRegistryAndFakeProviderExerciseCompleteSPI(t *testing.T) {
	registry := NewRegistry()
	var typedNil *fakeProvider
	if err := registry.Register(typedNil); err == nil {
		t.Fatal("typed-nil Provider 必须拒绝")
	}
	for _, provider := range []Provider{fakeProvider{id: "postgresql"}, fakeProvider{id: "mysql"}} {
		if err := registry.Register(provider); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.Register(fakeProvider{id: "postgresql"}); err == nil {
		t.Fatal("重复 Provider 必须拒绝")
	}
	descriptors := registry.Descriptors()
	if got := []string{descriptors[0].ID, descriptors[1].ID}; !reflect.DeepEqual(got, []string{"mysql", "postgresql"}) {
		t.Fatalf("Provider descriptor 必须稳定排序: %v", got)
	}

	_, ok := registry.Resolve("postgresql")
	if !ok {
		t.Fatal("未解析到 PostgreSQL fake Provider")
	}
	spec := testConnectionSpec("postgresql")
	material := &testMaterialSource{value: []byte("test-password")}
	pool, err := registry.OpenPool(context.Background(), spec, material)
	if err != nil {
		t.Fatal(err)
	}
	if material.calls != 1 {
		t.Fatalf("Provider 必须经 MaterialSource 获取凭证: %d", material.calls)
	}
	statement := databasev1.Statement{SQL: "select id from orders where id = ?", Parameters: []databasev1.Value{{Type: "int64", Value: json.RawMessage(`"1"`)}}}
	query, err := pool.Query(context.Background(), statement, 100)
	if err != nil || databasev1.ValidateQueryResult(query) != nil {
		t.Fatalf("fake query 契约失败: result=%+v err=%v", query, err)
	}
	if result, err := pool.Execute(context.Background(), statement); err != nil || result.RowsAffected != 1 {
		t.Fatalf("fake execute 契约失败: result=%+v err=%v", result, err)
	}
	transaction, err := pool.Begin(context.Background(), databasev1.TransactionOptions{Isolation: "serializable", TimeoutMS: 30000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Query(context.Background(), statement, 100); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Query(context.Background(), statement, 100); err == nil {
		t.Fatal("已结束事务必须返回 transaction lost")
	} else if code, retryable := ErrorDetails(err); code != databasev1.ErrorTransactionLost || !retryable {
		t.Fatalf("事务错误分类不稳定: code=%s retryable=%t", code, retryable)
	}
	if err := pool.Close(); err != nil || pool.Stats().Healthy {
		t.Fatalf("fake pool 关闭失败: %v", err)
	}
	missing := testConnectionSpec("oracle")
	if _, err := registry.OpenPool(context.Background(), missing, material); err == nil {
		t.Fatal("未注册 Provider 必须 fail-closed")
	} else if code, _ := ErrorDetails(err); code != databasev1.ErrorProviderNotFound {
		t.Fatalf("Provider not found 错误码=%s", code)
	}
}

func TestRegistryFreezesValidatedDescriptor(t *testing.T) {
	provider := &fakeProvider{id: "postgresql"}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	provider.id = "mysql"
	descriptors := registry.Descriptors()
	if len(descriptors) != 1 || descriptors[0].ID != "postgresql" {
		t.Fatalf("注册后的 descriptor 不得漂移: %+v", descriptors)
	}
	descriptors[0].ConfigurationSchema[0] = '['
	if !json.Valid(registry.Descriptors()[0].ConfigurationSchema) {
		t.Fatal("调用方不得修改 Registry 内冻结的 Schema")
	}
}

func TestServiceExposesOnlySafeProviderDiscovery(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(fakeProvider{id: "postgresql"}); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Providers([]byte(`{}`))
	if err != nil || len(result.Providers) != 1 || result.Providers[0].ID != "postgresql" {
		t.Fatalf("Provider discovery 失败: result=%+v err=%v", result, err)
	}
	if _, err := service.Providers([]byte(`{"secret":"forbidden"}`)); err == nil {
		t.Fatal("Provider discovery 必须严格拒绝额外字段")
	}
	contribution := service.Contribution()
	if contribution.ID != databasev1.Capability || !json.Valid(contribution.Descriptor) || len(contribution.Handlers) != 1 {
		t.Fatalf("Database Runtime contribution 无效: %+v", contribution)
	}
}
