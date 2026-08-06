package platformcontrolbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
)

type bootstrapSecret []byte

func (s bootstrapSecret) WithSecret(_ context.Context, use func([]byte) error) error {
	value := append([]byte(nil), s...)
	defer clear(value)
	return use(value)
}

type bootstrapProvider struct {
	providerID string
	pools      []*bootstrapPool
	specs      []databasev1.ConnectionSpec
	probeErr   error
}

func (p *bootstrapProvider) Descriptor() databasev1.ProviderDescriptor {
	providerID := p.providerID
	if providerID == "" {
		providerID = "mysql"
	}
	return databasev1.ProviderDescriptor{ID: providerID, Version: "1.0.0", DisplayName: "test database", ConfigurationSchema: json.RawMessage(`{"type":"object"}`), Capabilities: databasev1.ProviderCapabilities{Query: true, Execute: true, Transactions: true}}
}
func (*bootstrapProvider) Validate(context.Context, databasev1.ConnectionSpec) error { return nil }
func (p *bootstrapProvider) OpenPool(ctx context.Context, spec databasev1.ConnectionSpec, material databaseruntime.MaterialSource) (databaseruntime.Pool, error) {
	if err := material.WithMaterial(ctx, func(value databaseruntime.CredentialMaterial) error {
		if string(value.Bytes()) != "secret" {
			return errors.New("secret mismatch")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	pool := &bootstrapPool{probeErr: p.probeErr}
	p.pools = append(p.pools, pool)
	p.specs = append(p.specs, spec)
	return pool, nil
}

func TestBootstrapperProvisionsMySQLThroughServerConnection(t *testing.T) {
	provider := &bootstrapProvider{providerID: "mysql"}
	registry := databaseruntime.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	bootstrapper, _ := New(registry)
	profile := bootstrapProfile("mysql", "db.internal:3306", "platform", "platform", "vastplan", "verify-ca", "/run/vastplan/password")
	if err := bootstrapper.Provision(context.Background(), profile, bootstrapSecret("secret")); err != nil {
		t.Fatal(err)
	}
	if len(provider.specs) != 1 || provider.specs[0].Database != "" || provider.specs[0].Pool.MaxOpen != 1 {
		t.Fatalf("MySQL 建库必须使用无默认库的单连接管理池: %+v", provider.specs)
	}
	if got := strings.Join(provider.pools[0].statements, "\n"); !strings.Contains(got, "CREATE DATABASE IF NOT EXISTS `platform`") {
		t.Fatalf("MySQL 建库语句错误: %s", got)
	}
	if !provider.pools[0].closed {
		t.Fatal("管理连接池必须关闭")
	}
}

func TestBootstrapperProvisionsPostgreSQLThroughMaintenanceDatabase(t *testing.T) {
	provider := &bootstrapProvider{providerID: "postgresql"}
	registry := databaseruntime.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	bootstrapper, _ := New(registry)
	profile := bootstrapProfile("postgresql", "db.internal:5432", "platform", "platform", "vastplan", "verify-ca", "/run/vastplan/password")
	if err := bootstrapper.Provision(context.Background(), profile, bootstrapSecret("secret")); err != nil {
		t.Fatal(err)
	}
	if len(provider.specs) != 1 || provider.specs[0].Database != "postgres" || provider.specs[0].Pool.MaxOpen != 1 {
		t.Fatalf("PostgreSQL 建库必须使用 postgres 单连接管理池: %+v", provider.specs)
	}
	if got := strings.Join(provider.pools[0].statements, "\n"); !strings.Contains(got, `CREATE DATABASE "platform"`) {
		t.Fatalf("PostgreSQL 建库语句错误: %s", got)
	}
}

func TestPostgreSQLConcurrentCreateTreatsDuplicateDatabaseAsSuccess(t *testing.T) {
	err := databaseruntime.NewRuntimeError(databasev1.ErrorQueryFailed, false, &pgconn.PgError{Code: "42P04"})
	if !databaseAlreadyExists(err) {
		t.Fatal("并发创建 PostgreSQL 数据库时 duplicate_database 必须幂等收敛")
	}
}

type bootstrapPool struct {
	statements []string
	closed     bool
	probeErr   error
}

func (p *bootstrapPool) Probe(context.Context) error {
	p.statements = append(p.statements, "probe")
	return p.probeErr
}
func (p *bootstrapPool) Query(_ context.Context, statement databasev1.Statement, _ int) (databasev1.QueryResult, error) {
	p.statements = append(p.statements, statement.SQL)
	if strings.Contains(statement.SQL, "GET_LOCK") || strings.Contains(statement.SQL, "RELEASE_LOCK") {
		return databasev1.QueryResult{Rows: [][]databasev1.Value{{{Type: "int64", Value: json.RawMessage(`"1"`)}}}}, nil
	}
	return databasev1.QueryResult{}, nil
}
func (p *bootstrapPool) Execute(_ context.Context, statement databasev1.Statement) (databasev1.ExecuteResult, error) {
	p.statements = append(p.statements, statement.SQL)
	return databasev1.ExecuteResult{RowsAffected: 1}, nil
}
func (*bootstrapPool) Begin(context.Context, databasev1.TransactionOptions) (databaseruntime.Transaction, error) {
	return nil, errors.New("not used")
}
func (*bootstrapPool) Stats() databaseruntime.PoolStats {
	return databaseruntime.PoolStats{Healthy: true}
}
func (p *bootstrapPool) Close() error { p.closed = true; return nil }
func (p *bootstrapPool) WithPinned(ctx context.Context, work func(databaseruntime.PinnedSession) error) error {
	return work(p)
}

func TestBootstrapperTestsInitializesAndOpensQualifiedSharedState(t *testing.T) {
	provider := &bootstrapProvider{}
	registry := databaseruntime.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	bootstrapper, _ := New(registry)
	profile := bootstrapProfile("mysql", "db.internal:3306", "platform", "platform", "vastplan", "verify-ca", "/run/vastplan/password")
	secret := bootstrapSecret("secret")
	if err := bootstrapper.Test(context.Background(), profile, secret); err != nil || len(provider.pools) != 1 || !provider.pools[0].closed {
		t.Fatalf("连接测试应关闭候选池: pools=%+v err=%v", provider.pools, err)
	}
	store, err := bootstrapper.Initialize(context.Background(), profile, secret)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(provider.pools[1].statements, "\n")
	if !strings.Contains(joined, "GET_LOCK") || !strings.Contains(joined, "`platform`.`vastplan_shared_state`") {
		t.Fatalf("初始化缺少迁移锁或限定 schema: %s", joined)
	}
	if err := store.Close(); err != nil || !provider.pools[1].closed {
		t.Fatalf("受管 Store 必须关闭连接池: %v", err)
	}
	opened, err := bootstrapper.Open(context.Background(), profile, secret)
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(provider.pools[2].statements, "\n"); !strings.Contains(joined, "SELECT 1 FROM `platform`.`vastplan_shared_state`") {
		t.Fatalf("Open 必须复核目标表: %s", joined)
	}
	_ = opened.Close()
}

func TestBootstrapperRejectsMySQLCrossDatabaseSchema(t *testing.T) {
	registry := databaseruntime.NewRegistry()
	_ = registry.Register(&bootstrapProvider{})
	bootstrapper, _ := New(registry)
	profile := bootstrapProfile("mysql", "db.internal:3306", "platform", "other", "vastplan", "verify-ca", "/run/vastplan/password")
	if _, err := bootstrapper.Initialize(context.Background(), profile, bootstrapSecret("secret")); err == nil {
		t.Fatal("MySQL 控制 schema 不得越出连接 database")
	}
}

var _ platformcontrol.SecretSource = bootstrapSecret(nil)
var _ databaseruntime.Provider = (*bootstrapProvider)(nil)
var _ databaseruntime.Pool = (*bootstrapPool)(nil)
var _ databaseruntime.PinnedPool = (*bootstrapPool)(nil)

func bootstrapProfile(providerID, endpoint, database, schema, username, tlsMode, secretPath string) platformcontrolv1.Profile {
	options, _ := json.Marshal(map[string]any{"user": username, "tlsMode": tlsMode, "connectTimeoutMs": 10_000})
	return platformcontrolv1.Profile{
		SchemaVersion: 1, Generation: 1,
		Connection: databasev1.ConnectionCandidate{ProviderID: providerID, Endpoint: endpoint, Database: database, Options: options, Pool: databasev1.DefaultPoolPolicy()},
		Schema:     schema, SecretRef: platformcontrolv1.SecretRef{Kind: "owner-file", Path: secretPath}, ContractRange: "^1.0.0",
	}
}
