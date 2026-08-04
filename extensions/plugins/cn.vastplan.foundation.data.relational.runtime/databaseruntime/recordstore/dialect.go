package recordstore

import (
	"fmt"
	"strings"
)

type Dialect interface {
	ProviderID() string
	Quote(string) string
	Placeholder(int) string
	Boolean(bool) string
}

type postgresDialect struct{}

func (postgresDialect) ProviderID() string { return "postgresql" }
func (postgresDialect) Quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
func (postgresDialect) Placeholder(index int) string { return fmt.Sprintf("$%d", index) }
func (postgresDialect) Boolean(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}

type mysqlDialect struct{}

func (mysqlDialect) ProviderID() string { return "mysql" }
func (mysqlDialect) Quote(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}
func (mysqlDialect) Placeholder(int) string { return "?" }
func (mysqlDialect) Boolean(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}

func DialectFor(providerID string) (Dialect, error) {
	switch providerID {
	case "postgresql":
		return postgresDialect{}, nil
	case "mysql":
		return mysqlDialect{}, nil
	default:
		return nil, fmt.Errorf("Record Store 不支持 Provider %q", providerID)
	}
}
