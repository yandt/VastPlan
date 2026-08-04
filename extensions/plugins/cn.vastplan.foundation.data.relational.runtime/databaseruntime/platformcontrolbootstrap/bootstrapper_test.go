package platformcontrolbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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

type bootstrapProvider struct{ pools []*bootstrapPool }

func (*bootstrapProvider) Descriptor() databasev1.ProviderDescriptor {
	return databasev1.ProviderDescriptor{ID: "mysql", Version: "1.0.0", DisplayName: "test mysql", ConfigurationSchema: json.RawMessage(`{"type":"object"}`), Capabilities: databasev1.ProviderCapabilities{Query: true, Execute: true, Transactions: true}}
}
func (*bootstrapProvider) Validate(context.Context, databasev1.ConnectionSpec) error { return nil }
func (p *bootstrapProvider) OpenPool(ctx context.Context, _ databasev1.ConnectionSpec, material databaseruntime.MaterialSource) (databaseruntime.Pool, error) {
	if err := material.WithMaterial(ctx, func(value databaseruntime.CredentialMaterial) error {
		if string(value.Bytes()) != "secret" {
			return errors.New("secret mismatch")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	pool := &bootstrapPool{}
	p.pools = append(p.pools, pool)
	return pool, nil
}

type bootstrapPool struct {
	statements []string
	closed     bool
}

func (p *bootstrapPool) Probe(context.Context) error {
	p.statements = append(p.statements, "probe")
	return nil
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
	profile := platformcontrolv1.Profile{
		SchemaVersion: 1, Generation: 1, ProviderID: "mysql", Endpoint: "db.internal:3306", Database: "platform", Schema: "platform",
		TLS: platformcontrolv1.TLS{Mode: "verify-ca"}, Username: "vastplan", SecretRef: platformcontrolv1.SecretRef{Kind: "owner-file", Path: "/run/vastplan/password"}, ContractRange: "^1.0.0",
	}
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
	profile := platformcontrolv1.Profile{SchemaVersion: 1, Generation: 1, ProviderID: "mysql", Endpoint: "db.internal:3306", Database: "platform", Schema: "other", TLS: platformcontrolv1.TLS{Mode: "verify-ca"}, Username: "vastplan", SecretRef: platformcontrolv1.SecretRef{Kind: "owner-file", Path: "/run/vastplan/password"}, ContractRange: "^1.0.0"}
	if _, err := bootstrapper.Initialize(context.Background(), profile, bootstrapSecret("secret")); err == nil {
		t.Fatal("MySQL 控制 schema 不得越出连接 database")
	}
}

var _ platformcontrol.SecretSource = bootstrapSecret(nil)
var _ databaseruntime.Provider = (*bootstrapProvider)(nil)
var _ databaseruntime.Pool = (*bootstrapPool)(nil)
var _ databaseruntime.PinnedPool = (*bootstrapPool)(nil)
