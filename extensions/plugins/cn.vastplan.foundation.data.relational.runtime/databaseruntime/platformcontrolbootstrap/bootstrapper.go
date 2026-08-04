// Package platformcontrolbootstrap adapts the trusted Platform Control
// bootstrap workflow to Database Runtime's first-party Provider SPI.
package platformcontrolbootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	platformcontrol "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/sqlsharedstate"
)

type Bootstrapper struct{ registry *databaseruntime.Registry }

func New(registry *databaseruntime.Registry) (*Bootstrapper, error) {
	if registry == nil {
		return nil, errors.New("Platform Control Bootstrapper 需要 Database Provider Registry")
	}
	return &Bootstrapper{registry: registry}, nil
}

func (b *Bootstrapper) Test(ctx context.Context, profile platformcontrolv1.Profile, secret platformcontrol.SecretSource) error {
	pool, _, err := b.openPool(ctx, profile, secret)
	if err != nil {
		return err
	}
	defer pool.Close()
	return pool.Probe(ctx)
}

func (b *Bootstrapper) Initialize(ctx context.Context, profile platformcontrolv1.Profile, secret platformcontrol.SecretSource) (platformcontrol.ManagedStore, error) {
	pool, dialect, err := b.openPool(ctx, profile, secret)
	if err != nil {
		return nil, err
	}
	if err := pool.Probe(ctx); err != nil {
		_ = pool.Close()
		return nil, err
	}
	if err := initializeSchema(ctx, pool, dialect, profile); err != nil {
		_ = pool.Close()
		return nil, err
	}
	return newManagedStore(pool, dialect, profile.Schema)
}

func (b *Bootstrapper) Open(ctx context.Context, profile platformcontrolv1.Profile, secret platformcontrol.SecretSource) (platformcontrol.ManagedStore, error) {
	pool, dialect, err := b.openPool(ctx, profile, secret)
	if err != nil {
		return nil, err
	}
	if err := pool.Probe(ctx); err != nil {
		_ = pool.Close()
		return nil, err
	}
	health, err := sqlsharedstate.HealthStatement(dialect, profile.Schema)
	if err == nil {
		_, err = pool.Query(ctx, health, 1)
	}
	if err != nil {
		_ = pool.Close()
		return nil, err
	}
	return newManagedStore(pool, dialect, profile.Schema)
}

func (b *Bootstrapper) openPool(ctx context.Context, profile platformcontrolv1.Profile, secret platformcontrol.SecretSource) (databaseruntime.Pool, recordstore.Dialect, error) {
	if err := platformcontrolv1.ValidateProfile(profile); err != nil || secret == nil {
		return nil, nil, errors.New("Platform Control Bootstrap 输入无效")
	}
	if profile.ProviderID == "mysql" && profile.Schema != profile.Database {
		return nil, nil, errors.New("MySQL Platform Control schema 必须等于连接 database")
	}
	options := map[string]any{
		"user": profile.Username, "tlsMode": profile.TLS.Mode, "serverName": profile.TLS.ServerName,
		"connectTimeoutMs": 10_000,
	}
	if profile.ProviderID == "postgresql" {
		options["applicationName"] = "vastplan-platform-control"
	}
	raw, _ := json.Marshal(options)
	spec := databasev1.ConnectionSpec{
		Ref:        databasev1.ConnectionRef{ResourceID: "platform.control", Revision: profile.Generation},
		ProviderID: profile.ProviderID, Endpoint: profile.Endpoint, Database: profile.Database, Options: raw,
		Credentials: commonv1.ManagedCredentialRef{Handle: "credential://managed/platform-control-bootstrap", Scope: "service", Owner: databaseruntime.PluginID, Purpose: databasev1.CredentialPurpose, Version: int64(profile.Generation)},
		Pool:        databasev1.PoolPolicy{MinIdle: 1, MaxIdle: 4, MaxOpen: 16, MaxLifetimeMS: 30 * 60_000, MaxIdleTimeMS: 5 * 60_000, AcquireTimeoutMS: 10_000, IdlePoolTTLMS: 10 * 60_000},
	}
	pool, err := b.registry.OpenPool(ctx, spec, secretMaterialSource{source: secret})
	if err != nil {
		return nil, nil, err
	}
	dialect, err := recordstore.DialectFor(profile.ProviderID)
	if err != nil {
		_ = pool.Close()
		return nil, nil, err
	}
	return pool, dialect, nil
}

func initializeSchema(ctx context.Context, pool databaseruntime.Pool, dialect recordstore.Dialect, profile platformcontrolv1.Profile) error {
	pinned, ok := pool.(databaseruntime.PinnedPool)
	if !ok {
		return errors.New("Platform Control Provider 不支持 pinned schema session")
	}
	return pinned.WithPinned(ctx, func(session databaseruntime.PinnedSession) error {
		if dialect.ProviderID() == "postgresql" {
			transaction, err := session.Begin(ctx, databasev1.TransactionOptions{Isolation: "serializable", TimeoutMS: 60_000})
			if err != nil {
				return err
			}
			if err := initializeSchemaSession(ctx, transaction, dialect, profile); err != nil {
				_ = transaction.Rollback(context.Background())
				return err
			}
			return transaction.Commit(ctx)
		}
		lock, err := session.Query(ctx, recordstore.SchemaLockStatement(dialect), 1)
		if err != nil {
			return err
		}
		if err := recordstore.VerifyLockResult(lock); err != nil {
			return err
		}
		defer session.Query(context.Background(), recordstore.SchemaUnlockStatement(dialect), 1)
		return initializeSchemaSession(ctx, session, dialect, profile)
	})
}

func initializeSchemaSession(ctx context.Context, session recordstore.Session, dialect recordstore.Dialect, profile platformcontrolv1.Profile) error {
	if dialect.ProviderID() == "postgresql" {
		lock, err := session.Query(ctx, recordstore.SchemaLockStatement(dialect), 1)
		if err != nil {
			return err
		}
		if err := recordstore.VerifyLockResult(lock); err != nil {
			return err
		}
		if _, err := session.Execute(ctx, databasev1.Statement{SQL: "CREATE SCHEMA IF NOT EXISTS " + dialect.Quote(profile.Schema), Parameters: []databasev1.Value{}}); err != nil {
			return err
		}
	}
	statements, err := sqlsharedstate.SchemaStatementsInSchema(dialect, profile.Schema)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := session.Execute(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

type secretMaterialSource struct{ source platformcontrol.SecretSource }
type borrowedMaterial []byte

func (m borrowedMaterial) Bytes() []byte { return m }
func (s secretMaterialSource) WithMaterial(ctx context.Context, use func(databaseruntime.CredentialMaterial) error) error {
	return s.source.WithSecret(ctx, func(value []byte) error { return use(borrowedMaterial(value)) })
}

type poolSessions struct{ pool databaseruntime.Pool }

func (p poolSessions) Read(ctx context.Context, work func(recordstore.Session) error) error {
	return work(p.pool)
}

func (p poolSessions) Write(ctx context.Context, work func(recordstore.Session) error) error {
	transaction, err := p.pool.Begin(ctx, databasev1.TransactionOptions{Isolation: "serializable", TimeoutMS: 30_000})
	if err != nil {
		return err
	}
	if err := work(transaction); err != nil {
		_ = transaction.Rollback(context.Background())
		return err
	}
	return transaction.Commit(ctx)
}

type managedStore struct {
	*sqlsharedstate.Store
	pool databaseruntime.Pool
	once sync.Once
	err  error
}

func newManagedStore(pool databaseruntime.Pool, dialect recordstore.Dialect, schema string) (*managedStore, error) {
	store, err := sqlsharedstate.NewStoreInSchema(dialect, poolSessions{pool: pool}, schema)
	if err != nil {
		_ = pool.Close()
		return nil, err
	}
	return &managedStore{Store: store, pool: pool}, nil
}

func (s *managedStore) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() { s.err = s.pool.Close() })
	return s.err
}

var _ platformcontrol.Bootstrapper = (*Bootstrapper)(nil)
var _ platformcontrol.ManagedStore = (*managedStore)(nil)
