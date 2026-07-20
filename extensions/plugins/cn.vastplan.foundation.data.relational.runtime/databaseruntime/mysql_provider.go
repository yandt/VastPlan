package databaseruntime

import (
	"context"
	"crypto/tls"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"time"

	mysql "github.com/go-sql-driver/mysql"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

const mysqlOptionsSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "type":"object","additionalProperties":false,
  "properties":{
    "user":{"type":"string","minLength":1,"maxLength":128},
    "network":{"type":"string","enum":["tcp","unix"],"default":"tcp"},
    "tlsMode":{"type":"string","enum":["verify-full","disable"],"default":"verify-full"},
    "serverName":{"type":"string","maxLength":253},
    "connectTimeoutMs":{"type":"integer","minimum":100,"maximum":300000,"default":10000},
    "readTimeoutMs":{"type":"integer","minimum":0,"maximum":300000,"default":0},
    "writeTimeoutMs":{"type":"integer","minimum":0,"maximum":300000,"default":0},
    "rejectReadOnly":{"type":"boolean","default":false}
  },
  "required":["user"]
}`

type mysqlProvider struct{ policy ProviderSecurityPolicy }

func NewMySQLProvider(policy ProviderSecurityPolicy) Provider {
	return &mysqlProvider{policy: policy}
}

func (*mysqlProvider) Descriptor() databasev1.ProviderDescriptor {
	return databasev1.ProviderDescriptor{
		ID: "mysql", Version: "1.10.0", DisplayName: "MySQL",
		ConfigurationSchema: json.RawMessage(mysqlOptionsSchema),
		Capabilities: databasev1.ProviderCapabilities{
			Query: true, Execute: true, Transactions: true, ReadOnlyTransactions: true,
		},
	}
}

func (p *mysqlProvider) Validate(_ context.Context, spec databasev1.ConnectionSpec) error {
	if err := databasev1.ValidateConnectionSpec(spec); err != nil {
		return err
	}
	if spec.ProviderID != "mysql" {
		return errors.New("MySQL Provider ID 不匹配")
	}
	_, err := p.connectionConfig(spec)
	return err
}

func (p *mysqlProvider) OpenPool(_ context.Context, spec databasev1.ConnectionSpec, material MaterialSource) (Pool, error) {
	base, err := p.connectionConfig(spec)
	if err != nil {
		return nil, err
	}
	connector := &materialConnector{material: material, factory: func(secret []byte) (driver.Connector, func(), error) {
		candidate := base.Clone()
		candidate.Passwd = string(secret)
		inner, err := mysql.NewConnector(candidate)
		cleanup := func() { candidate.Passwd = "" }
		return inner, cleanup, err
	}}
	return newSQLPool(connector, spec.Pool)
}

func (p *mysqlProvider) connectionConfig(spec databasev1.ConnectionSpec) (*mysql.Config, error) {
	var options providerOptions
	if err := decodeProviderOptions(spec.Options, &options); err != nil {
		return nil, err
	}
	if options.ApplicationName != "" {
		return nil, errors.New("MySQL options 包含不支持的 applicationName")
	}
	if err := enforceTLSMode(options, p.policy); err != nil {
		return nil, err
	}
	if options.Network == "" {
		options.Network = "tcp"
	}
	config := mysql.NewConfig()
	config.User = options.User
	config.DBName = spec.Database
	config.Net = options.Network
	config.Timeout = time.Duration(options.ConnectTimeoutMS) * time.Millisecond
	config.ReadTimeout = time.Duration(options.ReadTimeoutMS) * time.Millisecond
	config.WriteTimeout = time.Duration(options.WriteTimeoutMS) * time.Millisecond
	config.ParseTime = true
	config.Loc = time.UTC
	config.RejectReadOnly = options.RejectReadOnly
	config.AllowAllFiles = false
	config.AllowCleartextPasswords = false
	config.AllowFallbackToPlaintext = false
	config.AllowOldPasswords = false
	config.InterpolateParams = false
	config.MultiStatements = false
	if options.Network == "unix" {
		if spec.Endpoint == "" || spec.Endpoint[0] != '/' {
			return nil, errors.New("MySQL unix endpoint 必须是绝对 socket 路径")
		}
		config.Addr = spec.Endpoint
	} else if options.Network == "tcp" {
		host, port, err := tcpEndpoint(spec.Endpoint, 3306)
		if err != nil {
			return nil, err
		}
		config.Addr = net.JoinHostPort(host, strconv.Itoa(int(port)))
		if options.TLSMode == "verify-full" {
			serverName := options.ServerName
			if serverName == "" {
				serverName = host
			}
			config.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
		}
	} else {
		return nil, errors.New("MySQL network 仅支持 tcp 或 unix")
	}
	if options.Network == "unix" && options.TLSMode == "verify-full" {
		if options.ServerName == "" {
			return nil, errors.New("MySQL unix + verify-full 必须提供 serverName")
		}
		config.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: options.ServerName}
	}
	return config, nil
}
