package databaseruntime

import (
	"context"
	"database/sql/driver"
	"errors"
)

const maximumDatabaseCredentialBytes = 64 * 1024

type connectorFactory func([]byte) (driver.Connector, func(), error)

// materialConnector obtains credential material separately for every physical
// connection. The long-lived database/sql pool retains only this connector and
// the MaterialSource, never a password-bearing DSN or driver configuration.
type materialConnector struct {
	material MaterialSource
	factory  connectorFactory
}

func (c *materialConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if c == nil || ctx == nil || nilInterface(c.material) || c.factory == nil {
		return nil, errors.New("Database connector 参数无效")
	}
	var connection driver.Conn
	err := c.material.WithMaterial(ctx, func(material CredentialMaterial) error {
		value := material.Bytes()
		if len(value) == 0 || len(value) > maximumDatabaseCredentialBytes {
			return errors.New("数据库凭证 material 长度无效")
		}
		connector, cleanup, err := c.factory(value)
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			return err
		}
		if nilInterface(connector) {
			return errors.New("数据库驱动返回空 Connector")
		}
		connection, err = connector.Connect(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	if connection == nil {
		return nil, errors.New("数据库驱动返回空物理连接")
	}
	return connection, nil
}

func (*materialConnector) Driver() driver.Driver { return connectorOnlyDriver{} }

type connectorOnlyDriver struct{}

func (connectorOnlyDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("Database Runtime 只允许通过受控 Connector 建立连接")
}
