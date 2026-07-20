package databaseruntime

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

const postgresqlOptionsSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "type":"object","additionalProperties":false,
  "properties":{
    "user":{"type":"string","minLength":1,"maxLength":128},
    "tlsMode":{"type":"string","enum":["verify-full","disable"],"default":"verify-full"},
    "serverName":{"type":"string","maxLength":253},
    "connectTimeoutMs":{"type":"integer","minimum":100,"maximum":300000,"default":10000},
    "applicationName":{"type":"string","maxLength":128}
  },
  "required":["user"]
}`

type postgresqlProvider struct{ policy ProviderSecurityPolicy }

func NewPostgreSQLProvider(policy ProviderSecurityPolicy) Provider {
	return &postgresqlProvider{policy: policy}
}

func (*postgresqlProvider) Descriptor() databasev1.ProviderDescriptor {
	return databasev1.ProviderDescriptor{
		ID: "postgresql", Version: "5.10.0", DisplayName: "PostgreSQL",
		ConfigurationSchema: json.RawMessage(postgresqlOptionsSchema),
		Capabilities: databasev1.ProviderCapabilities{
			Query: true, Execute: true, Transactions: true, ReadOnlyTransactions: true,
		},
	}
}

func (p *postgresqlProvider) Validate(_ context.Context, spec databasev1.ConnectionSpec) error {
	if err := databasev1.ValidateConnectionSpec(spec); err != nil {
		return err
	}
	if spec.ProviderID != "postgresql" {
		return errors.New("PostgreSQL Provider ID 不匹配")
	}
	_, err := p.connectionConfig(spec)
	return err
}

func (p *postgresqlProvider) OpenPool(_ context.Context, spec databasev1.ConnectionSpec, material MaterialSource) (Pool, error) {
	base, err := p.connectionConfig(spec)
	if err != nil {
		return nil, err
	}
	connector := &materialConnector{material: material, factory: func(secret []byte) (driver.Connector, func(), error) {
		candidate := base.Copy()
		candidate.Password = string(secret)
		cleanup := func() { candidate.Password = "" }
		return stdlib.GetConnector(*candidate), cleanup, nil
	}}
	return newSQLPool(connector, spec.Pool)
}

func (p *postgresqlProvider) connectionConfig(spec databasev1.ConnectionSpec) (*pgx.ConnConfig, error) {
	var options providerOptions
	if err := decodeProviderOptions(spec.Options, &options); err != nil {
		return nil, err
	}
	if options.Network != "" || options.ReadTimeoutMS != 0 || options.WriteTimeoutMS != 0 || options.RejectReadOnly {
		return nil, errors.New("PostgreSQL options 包含不支持的字段")
	}
	if err := enforceTLSMode(options, p.policy); err != nil {
		return nil, err
	}
	host, port, err := tcpEndpoint(spec.Endpoint, 5432)
	if err != nil {
		return nil, err
	}
	if os.Getenv("PGSERVICE") != "" || os.Getenv("PGSERVICEFILE") != "" {
		return nil, errors.New("Database Runtime 禁止通过 PGSERVICE/PGSERVICEFILE 注入连接配置")
	}
	address := &url.URL{
		Scheme: "postgres", User: url.UserPassword(options.User, "__vastplan_material_placeholder__"),
		Host: net.JoinHostPort(host, strconv.Itoa(int(port))),
		Path: "/" + spec.Database,
	}
	query := address.Query()
	// Parse a fully controlled non-secret template, then install TLS and material
	// explicitly. sslmode=disable prevents ambient PGSSL* files from being read.
	query.Set("sslmode", "disable")
	query.Set("passfile", "/__vastplan_disabled_pgpass")
	query.Set("target_session_attrs", "any")
	address.RawQuery = query.Encode()
	config, err := pgx.ParseConfigWithOptions(address.String(), pgx.ParseConfigOptions{ParseConfigOptions: pgconn.ParseConfigOptions{
		ConnStringAllowedKeys: []string{"user", "password", "host", "port", "database", "sslmode", "passfile", "target_session_attrs"},
	}})
	if err != nil {
		return nil, err
	}
	// Never accept ambient pgpass/service credentials or fallback hosts.
	config.Password = ""
	config.Fallbacks = nil
	config.ValidateConnect = nil
	config.KerberosSpn = ""
	config.KerberosSrvName = ""
	config.RequireAuth = ""
	config.ChannelBinding = "prefer"
	config.SSLNegotiation = "postgres"
	config.OAuthTokenProvider = nil
	config.ConnectTimeout = time.Duration(options.ConnectTimeoutMS) * time.Millisecond
	dialer := &net.Dialer{Timeout: config.ConnectTimeout, KeepAlive: 30 * time.Second}
	config.DialFunc = dialer.DialContext
	config.RuntimeParams = map[string]string{}
	if options.ApplicationName != "" {
		config.RuntimeParams["application_name"] = options.ApplicationName
	}
	if options.TLSMode == "verify-full" {
		serverName := options.ServerName
		if serverName == "" {
			serverName = host
		}
		config.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	} else {
		config.TLSConfig = nil
	}
	return config, nil
}
