package platformcontrolbootstrap

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	platformcontrol "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

// Provision creates only the target logical database. It never creates tables,
// commits a Profile, binds Shared State, or removes an existing database.
func (b *Bootstrapper) Provision(ctx context.Context, profile platformcontrolv1.Profile, secret platformcontrol.SecretSource) error {
	if err := validateBootstrapInput(profile, secret); err != nil {
		return err
	}
	spec, err := connectionSpec(profile, true)
	if err != nil {
		return err
	}
	pool, err := b.registry.OpenPool(ctx, spec, secretMaterialSource{source: secret})
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Probe(ctx); err != nil {
		return err
	}
	dialect, err := recordstore.DialectFor(profile.Connection.ProviderID)
	if err != nil {
		return err
	}
	statement := "CREATE DATABASE " + dialect.Quote(profile.Connection.Database)
	if profile.Connection.ProviderID == "mysql" {
		statement = "CREATE DATABASE IF NOT EXISTS " + dialect.Quote(profile.Connection.Database)
	}
	_, err = pool.Execute(ctx, databasev1.Statement{SQL: statement, Parameters: []databasev1.Value{}})
	if err != nil && !databaseAlreadyExists(err) {
		return err
	}
	return nil
}

func validateBootstrapInput(profile platformcontrolv1.Profile, secret platformcontrol.SecretSource) error {
	if err := platformcontrolv1.ValidateProfile(profile); err != nil || secret == nil {
		return errors.New("Platform Control Bootstrap 输入无效")
	}
	if profile.Connection.ProviderID == "mysql" && profile.Schema != profile.Connection.Database {
		return errors.New("MySQL Platform Control schema 必须等于连接 database")
	}
	return nil
}

func connectionSpec(profile platformcontrolv1.Profile, provisioning bool) (databasev1.ConnectionSpec, error) {
	var options map[string]any
	if err := json.Unmarshal(profile.Connection.Options, &options); err != nil || options == nil {
		return databasev1.ConnectionSpec{}, errors.New("Platform Control Provider options 无效")
	}
	database := profile.Connection.Database
	if profile.Connection.ProviderID == "postgresql" {
		options["applicationName"] = "vastplan-platform-control"
		if provisioning {
			database = "postgres"
			delete(options, "searchPath")
		} else {
			options["searchPath"] = profile.Schema
		}
	} else if provisioning {
		database = ""
	}
	raw, err := json.Marshal(options)
	if err != nil {
		return databasev1.ConnectionSpec{}, err
	}
	pool := profile.Connection.Pool
	if provisioning {
		pool.MinIdle = 0
		pool.MaxIdle = 1
		pool.MaxOpen = 1
	}
	return databasev1.ConnectionSpec{
		Ref:        databasev1.ConnectionRef{ResourceID: "platform.control", Revision: profile.Generation},
		ProviderID: profile.Connection.ProviderID,
		Endpoint:   profile.Connection.Endpoint,
		Database:   database,
		Options:    raw,
		Credentials: commonv1.ManagedCredentialRef{
			Handle: "credential://managed/platform-control-bootstrap", Scope: "service", Owner: databaseruntime.PluginID,
			Purpose: databasev1.CredentialPurpose, Version: int64(profile.Generation),
		},
		Pool: pool,
	}, nil
}

func databaseAlreadyExists(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "42P04"
}
