package databaseruntime

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func TestDefaultRegistryContainsRealProviders(t *testing.T) {
	registry, err := NewDefaultRegistry(ProviderSecurityPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	descriptors := registry.Descriptors()
	if len(descriptors) != 2 || descriptors[0].ID != "mysql" || descriptors[1].ID != "postgresql" {
		t.Fatalf("默认 Provider 注册错误: %+v", descriptors)
	}
	for _, descriptor := range descriptors {
		if !json.Valid(descriptor.ConfigurationSchema) || !descriptor.Capabilities.Transactions {
			t.Fatalf("Provider descriptor 无效: %+v", descriptor)
		}
	}
}

func TestProvidersEnforceTLSAndStrictOptions(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		spec     databasev1.ConnectionSpec
	}{
		{name: "postgresql", provider: NewPostgreSQLProvider(ProviderSecurityPolicy{}), spec: providerSpec("postgresql", "db.internal:5432")},
		{name: "mysql", provider: NewMySQLProvider(ProviderSecurityPolicy{}), spec: providerSpec("mysql", "db.internal:3306")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.provider.Validate(context.Background(), test.spec); err != nil {
				t.Fatalf("安全默认配置被拒绝: %v", err)
			}
			insecure := test.spec
			insecure.Options = json.RawMessage(`{"user":"runtime","tlsMode":"disable"}`)
			if err := test.provider.Validate(context.Background(), insecure); err == nil {
				t.Fatal("默认策略必须拒绝关闭 TLS")
			}
			unknown := test.spec
			unknown.Options = json.RawMessage(`{"user":"runtime","unknown":true}`)
			if err := test.provider.Validate(context.Background(), unknown); err == nil {
				t.Fatal("未知 options 必须拒绝")
			}
		})
	}
	postgres := NewPostgreSQLProvider(ProviderSecurityPolicy{AllowInsecureTLS: true})
	postgresSpec := providerSpec("postgresql", "127.0.0.1:5432")
	postgresSpec.Options = json.RawMessage(`{"user":"runtime","tlsMode":"disable"}`)
	if err := postgres.Validate(context.Background(), postgresSpec); err != nil {
		t.Fatalf("显式测试策略应允许 PostgreSQL 无 TLS: %v", err)
	}
	mysqlInsecureProvider := NewMySQLProvider(ProviderSecurityPolicy{AllowInsecureTLS: true})
	mysqlSpec := providerSpec("mysql", "127.0.0.1:3306")
	mysqlSpec.Options = json.RawMessage(`{"user":"runtime","tlsMode":"disable"}`)
	if err := mysqlInsecureProvider.Validate(context.Background(), mysqlSpec); err != nil {
		t.Fatalf("显式测试策略应允许 MySQL 无 TLS: %v", err)
	}
	postgresConfig, err := postgres.(*postgresqlProvider).connectionConfig(providerSpec("postgresql", "db.internal:5432"))
	if err != nil || postgresConfig.Password != "" || postgresConfig.TLSConfig == nil || postgresConfig.TLSConfig.InsecureSkipVerify {
		t.Fatalf("PostgreSQL 基础配置不得持有密码且必须验证 TLS: config=%+v err=%v", postgresConfig, err)
	}
	mysqlConfig, err := mysqlInsecureProvider.(*mysqlProvider).connectionConfig(providerSpec("mysql", "db.internal:3306"))
	if err != nil || mysqlConfig.Passwd != "" || mysqlConfig.TLS == nil || mysqlConfig.TLS.InsecureSkipVerify {
		t.Fatalf("MySQL 基础配置不得持有密码且必须验证 TLS: config=%+v err=%v", mysqlConfig, err)
	}
}

func TestPostgreSQLProviderRejectsAmbientServiceInjection(t *testing.T) {
	t.Setenv("PGSERVICE", "attacker-controlled")
	provider := NewPostgreSQLProvider(ProviderSecurityPolicy{})
	if err := provider.Validate(context.Background(), providerSpec("postgresql", "db.internal:5432")); err == nil {
		t.Fatal("PGSERVICE 环境注入必须 fail-closed")
	}
}

func TestSQLPoolUsesMaterialPerPhysicalConnectionAndPreservesValues(t *testing.T) {
	material := &testMaterialSource{value: []byte("test-password")}
	backend := &stubSQLBackend{}
	connector := &materialConnector{material: material, factory: func(secret []byte) (driver.Connector, func(), error) {
		if string(secret) != "test-password" {
			t.Fatal("Connector 未收到正确的短时凭证")
		}
		return stubSQLConnector{backend: backend}, func() {}, nil
	}}
	pool, err := newSQLPool(connector, databasev1.PoolPolicy{
		MinIdle: 2, MaxIdle: 2, MaxOpen: 4, MaxLifetimeMS: 60_000,
		MaxIdleTimeMS: 30_000, AcquireTimeoutMS: 1_000, IdlePoolTTLMS: 60_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Probe(context.Background()); err != nil {
		t.Fatal(err)
	}
	if material.calls < 2 || backend.connections() < 2 {
		t.Fatalf("minIdle 未预热独立物理连接: material=%d connections=%d", material.calls, backend.connections())
	}
	statement := databasev1.Statement{SQL: "select values", Parameters: []databasev1.Value{
		{Type: "int64", Value: json.RawMessage(`"7"`)},
		{Type: "bytes", Value: json.RawMessage(`"AAE="`)},
	}}
	result, err := pool.Query(context.Background(), statement, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(result.Rows) != 1 {
		t.Fatalf("maxRows 截断错误: %+v", result)
	}
	wantTypes := []string{"int64", "decimal", "json", "bytes", "timestamp"}
	gotTypes := make([]string, len(result.Rows[0]))
	for index := range result.Rows[0] {
		gotTypes[index] = result.Rows[0][index].Type
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("结果值类型错误: got=%v want=%v", gotTypes, wantTypes)
	}
	if affected, err := pool.Execute(context.Background(), statement); err != nil || affected.RowsAffected != 2 {
		t.Fatalf("execute 失败: result=%+v err=%v", affected, err)
	}
	transaction, err := pool.Begin(context.Background(), databasev1.TransactionOptions{
		Isolation: "serializable", TimeoutMS: 1_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Query(context.Background(), statement, 1); err == nil {
		t.Fatal("结束后的事务必须 fail-closed")
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLValueConversionAndStableErrorClasses(t *testing.T) {
	parameters := []databasev1.Value{
		{Type: "null"},
		{Type: "string", Value: json.RawMessage(`"text"`)},
		{Type: "int64", Value: json.RawMessage(`"9223372036854775807"`)},
		{Type: "decimal", Value: json.RawMessage(`"12.3400"`)},
		{Type: "bool", Value: json.RawMessage(`true`)},
		{Type: "bytes", Value: json.RawMessage(`"AAE="`)},
		{Type: "timestamp", Value: json.RawMessage(`"2026-07-20T12:00:00Z"`)},
		{Type: "json", Value: json.RawMessage(`{"ok":true}`)},
	}
	arguments, err := statementArguments(databasev1.Statement{SQL: "select", Parameters: parameters})
	if err != nil || len(arguments) != len(parameters) {
		t.Fatalf("参数转换失败: %#v err=%v", arguments, err)
	}
	for _, test := range []struct {
		err         error
		transaction bool
		code        string
		retryable   bool
	}{
		{err: &pgconn.PgError{Code: "40001"}, transaction: true, code: databasev1.ErrorTransactionConflict, retryable: true},
		{err: &pgconn.PgError{Code: "23505"}, code: databasev1.ErrorQueryFailed, retryable: false},
		{err: &mysql.MySQLError{Number: 1213}, transaction: true, code: databasev1.ErrorTransactionConflict, retryable: true},
		{err: &mysql.MySQLError{Number: 1040}, code: databasev1.ErrorPoolExhausted, retryable: true},
	} {
		code, retryable := ErrorDetails(classifySQLError(test.err, test.transaction))
		if code != test.code || retryable != test.retryable {
			t.Fatalf("错误分类错误: code=%s retryable=%t", code, retryable)
		}
	}
}

func providerSpec(providerID, endpoint string) databasev1.ConnectionSpec {
	spec := testConnectionSpec(providerID)
	spec.Endpoint = endpoint
	spec.Options = json.RawMessage(`{"user":"runtime"}`)
	spec.Pool.MinIdle = 0
	return spec
}

type stubSQLBackend struct {
	mu      sync.Mutex
	connect int
}

func (b *stubSQLBackend) connected() {
	b.mu.Lock()
	b.connect++
	b.mu.Unlock()
}

func (b *stubSQLBackend) connections() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connect
}

type stubSQLConnector struct{ backend *stubSQLBackend }

func (c stubSQLConnector) Connect(context.Context) (driver.Conn, error) {
	c.backend.connected()
	return &stubSQLConn{}, nil
}
func (stubSQLConnector) Driver() driver.Driver { return stubSQLDriver{} }

type stubSQLDriver struct{}

func (stubSQLDriver) Open(string) (driver.Conn, error) { return &stubSQLConn{}, nil }

type stubSQLConn struct{ closed bool }

func (*stubSQLConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("prepare 未实现") }
func (c *stubSQLConn) Close() error                      { c.closed = true; return nil }
func (*stubSQLConn) Begin() (driver.Tx, error)           { return &stubSQLTx{}, nil }
func (c *stubSQLConn) Ping(context.Context) error {
	if c.closed {
		return driver.ErrBadConn
	}
	return nil
}
func (*stubSQLConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &stubSQLTx{}, nil
}
func (*stubSQLConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(2), nil
}
func (*stubSQLConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 123, time.UTC)
	return &stubSQLRows{rows: [][]driver.Value{
		{int64(1), []byte("12.3400"), []byte(`{"ok":true}`), []byte{0, 1}, now},
		{int64(2), []byte("99.9"), []byte(`{"ok":false}`), []byte{2, 3}, now},
	}}, nil
}

type stubSQLTx struct{}

func (*stubSQLTx) Commit() error   { return nil }
func (*stubSQLTx) Rollback() error { return nil }

type stubSQLRows struct {
	rows  [][]driver.Value
	index int
}

func (*stubSQLRows) Columns() []string {
	return []string{"id", "amount", "payload", "data", "created_at"}
}
func (*stubSQLRows) Close() error { return nil }
func (r *stubSQLRows) Next(destination []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(destination, r.rows[r.index])
	r.index++
	return nil
}
func (*stubSQLRows) ColumnTypeDatabaseTypeName(index int) string {
	return []string{"BIGINT", "NUMERIC", "JSON", "BYTEA", "TIMESTAMPTZ"}[index]
}
func (*stubSQLRows) ColumnTypeNullable(int) (bool, bool) { return true, true }
