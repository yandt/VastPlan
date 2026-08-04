// Package sqlsharedstate implements the existing sharedstate.Store port on a
// relational database. It is an internal Database Runtime module, not a new
// deployable plugin or a second plugin-facing state contract.
package sqlsharedstate

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

type TransactionProvider interface {
	Read(context.Context, func(recordstore.Session) error) error
	Write(context.Context, func(recordstore.Session) error) error
}

type Store struct {
	dialect  recordstore.Dialect
	sessions TransactionProvider
	schema   string
	now      func() time.Time
}

func NewStore(dialect recordstore.Dialect, sessions TransactionProvider) (*Store, error) {
	return NewStoreInSchema(dialect, sessions, "")
}

func NewStoreInSchema(dialect recordstore.Dialect, sessions TransactionProvider, schema string) (*Store, error) {
	if dialect == nil || sessions == nil {
		return nil, errors.New("SQL Shared State 依赖不能为空")
	}
	if schema != strings.TrimSpace(schema) || strings.ContainsAny(schema, "\x00\r\n") {
		return nil, errors.New("SQL Shared State schema 无效")
	}
	return &Store{dialect: dialect, sessions: sessions, schema: schema, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Store) Get(ctx context.Context, scope sharedstate.Scope, key string) (sharedstate.Entry, error) {
	if err := validateKey(scope, key); err != nil {
		return sharedstate.Entry{}, err
	}
	var entry sharedstate.Entry
	err := s.sessions.Read(ctx, func(session recordstore.Session) error {
		result, err := session.Query(ctx, s.selectOne(scope, key), 1)
		if err != nil {
			return err
		}
		entry, err = decodeEntry(key, result)
		return err
	})
	return entry, err
}

func (s *Store) Create(ctx context.Context, scope sharedstate.Scope, key string, value []byte) (sharedstate.Entry, error) {
	if err := validateWrite(scope, key, value); err != nil {
		return sharedstate.Entry{}, err
	}
	now := s.now().UTC()
	entry := sharedstate.Entry{Key: key, Value: append([]byte(nil), value...), Revision: 1, UpdatedAt: now}
	err := s.sessions.Write(ctx, func(session recordstore.Session) error {
		existing, err := session.Query(ctx, s.selectOne(scope, key), 1)
		if err != nil {
			return err
		}
		if len(existing.Rows) != 0 {
			return sharedstate.ErrConflict
		}
		result, err := session.Execute(ctx, s.insert(scope, entry))
		if err != nil {
			return mapConstraint(err)
		}
		if result.RowsAffected != 1 {
			return sharedstate.ErrConflict
		}
		return nil
	})
	return entry, err
}

func (s *Store) Update(ctx context.Context, scope sharedstate.Scope, key string, value []byte, expected uint64) (sharedstate.Entry, error) {
	if err := validateWrite(scope, key, value); err != nil || expected == 0 {
		if err != nil {
			return sharedstate.Entry{}, err
		}
		return sharedstate.Entry{}, sharedstate.ErrInvalid
	}
	entry := sharedstate.Entry{Key: key, Value: append([]byte(nil), value...), Revision: expected + 1, UpdatedAt: s.now().UTC()}
	err := s.sessions.Write(ctx, func(session recordstore.Session) error {
		result, err := session.Execute(ctx, s.update(scope, entry, expected))
		if err != nil {
			return err
		}
		if result.RowsAffected != 1 {
			return sharedstate.ErrConflict
		}
		return nil
	})
	return entry, err
}

func (s *Store) Delete(ctx context.Context, scope sharedstate.Scope, key string, expected uint64) error {
	if err := validateKey(scope, key); err != nil || expected == 0 {
		if err != nil {
			return err
		}
		return sharedstate.ErrInvalid
	}
	return s.sessions.Write(ctx, func(session recordstore.Session) error {
		result, err := session.Execute(ctx, s.delete(scope, key, expected))
		if err != nil {
			return err
		}
		if result.RowsAffected != 1 {
			existing, queryErr := session.Query(ctx, s.selectOne(scope, key), 1)
			if queryErr != nil {
				return queryErr
			}
			if len(existing.Rows) == 0 {
				return sharedstate.ErrNotFound
			}
			return sharedstate.ErrConflict
		}
		return nil
	})
}

func (s *Store) List(ctx context.Context, scope sharedstate.Scope, prefix string, limit int, cursor string) (sharedstate.Page, error) {
	if err := scope.Validate(); err != nil {
		return sharedstate.Page{}, err
	}
	if err := sharedstate.ValidateList(prefix, limit, cursor); err != nil {
		return sharedstate.Page{}, err
	}
	page := sharedstate.Page{Items: []sharedstate.Entry{}}
	err := s.sessions.Read(ctx, func(session recordstore.Session) error {
		result, err := session.Query(ctx, s.list(scope, prefix, limit+1, cursor), limit+1)
		if err != nil {
			return err
		}
		for index, row := range result.Rows {
			if index == limit {
				page.NextCursor = page.Items[len(page.Items)-1].Key
				break
			}
			entry, err := decodeRow(row)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, entry)
		}
		return nil
	})
	return page, err
}

func SchemaStatements(dialect recordstore.Dialect) []databasev1.Statement {
	statements, _ := SchemaStatementsInSchema(dialect, "")
	return statements
}

func SchemaStatementsInSchema(dialect recordstore.Dialect, schema string) ([]databasev1.Statement, error) {
	if dialect == nil || schema != strings.TrimSpace(schema) || strings.ContainsAny(schema, "\x00\r\n") {
		return nil, errors.New("SQL Shared State schema 无效")
	}
	table := qualifiedTable(dialect, schema, "vastplan_shared_state")
	if dialect.ProviderID() == "postgresql" {
		return []databasev1.Statement{{SQL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s ("scope_hash" BYTEA NOT NULL, "entry_key" TEXT NOT NULL, "value" BYTEA NOT NULL, "revision" BIGINT NOT NULL, "updated_at" TIMESTAMPTZ NOT NULL, PRIMARY KEY ("scope_hash", "entry_key"))`, table), Parameters: []databasev1.Value{}}}, nil
	}
	return []databasev1.Statement{{SQL: fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (`scope_hash` BINARY(32) NOT NULL, `entry_key` VARCHAR(320) NOT NULL, `value` LONGBLOB NOT NULL, `revision` BIGINT UNSIGNED NOT NULL, `updated_at` DATETIME(6) NOT NULL, PRIMARY KEY (`scope_hash`, `entry_key`))", table), Parameters: []databasev1.Value{}}}, nil
}

func HealthStatement(dialect recordstore.Dialect, schema string) (databasev1.Statement, error) {
	if dialect == nil || schema != strings.TrimSpace(schema) || strings.ContainsAny(schema, "\x00\r\n") {
		return databasev1.Statement{}, errors.New("SQL Shared State schema 无效")
	}
	return databasev1.Statement{SQL: "SELECT 1 FROM " + qualifiedTable(dialect, schema, "vastplan_shared_state") + " WHERE 1 = 0", Parameters: []databasev1.Value{}}, nil
}

func validateKey(scope sharedstate.Scope, key string) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	return sharedstate.ValidateKey(key)
}

func validateWrite(scope sharedstate.Scope, key string, value []byte) error {
	if err := validateKey(scope, key); err != nil {
		return err
	}
	return sharedstate.ValidateValue(value)
}

func (s *Store) identity(scope sharedstate.Scope) ([]string, []databasev1.Value) {
	digest := sha256.Sum256([]byte(string(scope.Kind) + "\x00" + scope.TenantID + "\x00" + scope.RuntimeScope + "\x00" + scope.PluginID + "\x00" + scope.Namespace))
	return []string{"scope_hash"}, []databasev1.Value{bytesValue(digest[:])}
}

func (s *Store) selectOne(scope sharedstate.Scope, key string) databasev1.Statement {
	columns, parameters := s.identity(scope)
	columns = append(columns, "entry_key")
	parameters = append(parameters, stringValue(key))
	return databasev1.Statement{SQL: fmt.Sprintf("SELECT %s, %s, %s FROM %s WHERE %s", s.dialect.Quote("value"), s.dialect.Quote("revision"), s.dialect.Quote("updated_at"), s.table(), equalWhere(s.dialect, columns)), Parameters: parameters}
}

func (s *Store) insert(scope sharedstate.Scope, entry sharedstate.Entry) databasev1.Statement {
	columns, parameters := s.identity(scope)
	columns = append(columns, "entry_key", "value", "revision", "updated_at")
	parameters = append(parameters, stringValue(entry.Key), bytesValue(entry.Value), intValue(entry.Revision), timestampValue(entry.UpdatedAt))
	return databasev1.Statement{SQL: fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", s.table(), quoteAll(s.dialect, columns), placeholders(s.dialect, len(columns))), Parameters: parameters}
}

func (s *Store) update(scope sharedstate.Scope, entry sharedstate.Entry, expected uint64) databasev1.Statement {
	columns, identity := s.identity(scope)
	columns = append(columns, "entry_key")
	identity = append(identity, stringValue(entry.Key))
	parameters := []databasev1.Value{bytesValue(entry.Value), intValue(entry.Revision), timestampValue(entry.UpdatedAt)}
	parameters = append(parameters, identity...)
	parameters = append(parameters, intValue(expected))
	return databasev1.Statement{SQL: fmt.Sprintf("UPDATE %s SET %s = %s, %s = %s, %s = %s WHERE %s AND %s = %s",
		s.table(), s.dialect.Quote("value"), s.dialect.Placeholder(1),
		s.dialect.Quote("revision"), s.dialect.Placeholder(2), s.dialect.Quote("updated_at"), s.dialect.Placeholder(3),
		equalWhereOffset(s.dialect, columns, 3), s.dialect.Quote("revision"), s.dialect.Placeholder(len(parameters))), Parameters: parameters}
}

func (s *Store) delete(scope sharedstate.Scope, key string, expected uint64) databasev1.Statement {
	columns, parameters := s.identity(scope)
	columns = append(columns, "entry_key")
	parameters = append(parameters, stringValue(key), intValue(expected))
	return databasev1.Statement{SQL: fmt.Sprintf("DELETE FROM %s WHERE %s AND %s = %s", s.table(), equalWhere(s.dialect, columns), s.dialect.Quote("revision"), s.dialect.Placeholder(len(parameters))), Parameters: parameters}
}

func (s *Store) list(scope sharedstate.Scope, prefix string, limit int, cursor string) databasev1.Statement {
	columns, parameters := s.identity(scope)
	where := equalWhere(s.dialect, columns)
	parameters = append(parameters, stringValue(escapeLike(prefix)+"%"), stringValue(cursor), intValue(uint64(limit)))
	where += fmt.Sprintf(" AND %s LIKE %s ESCAPE '\\\\' AND %s > %s", s.dialect.Quote("entry_key"), s.dialect.Placeholder(len(columns)+1), s.dialect.Quote("entry_key"), s.dialect.Placeholder(len(columns)+2))
	return databasev1.Statement{SQL: fmt.Sprintf("SELECT %s, %s, %s, %s FROM %s WHERE %s ORDER BY %s ASC LIMIT %s",
		s.dialect.Quote("entry_key"), s.dialect.Quote("value"), s.dialect.Quote("revision"), s.dialect.Quote("updated_at"),
		s.table(), where, s.dialect.Quote("entry_key"), s.dialect.Placeholder(len(columns)+3)), Parameters: parameters}
}

func (s *Store) table() string { return qualifiedTable(s.dialect, s.schema, "vastplan_shared_state") }

func qualifiedTable(dialect recordstore.Dialect, schema, table string) string {
	if schema == "" {
		return dialect.Quote(table)
	}
	return dialect.Quote(schema) + "." + dialect.Quote(table)
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func decodeEntry(key string, result databasev1.QueryResult) (sharedstate.Entry, error) {
	if len(result.Rows) == 0 {
		return sharedstate.Entry{}, sharedstate.ErrNotFound
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 3 {
		return sharedstate.Entry{}, errors.New("SQL Shared State Get 响应无效")
	}
	return decodeValues(key, result.Rows[0])
}

func decodeRow(row []databasev1.Value) (sharedstate.Entry, error) {
	if len(row) != 4 || row[0].Type != "string" {
		return sharedstate.Entry{}, errors.New("SQL Shared State List 响应无效")
	}
	var key string
	if json.Unmarshal(row[0].Value, &key) != nil {
		return sharedstate.Entry{}, errors.New("SQL Shared State key 无效")
	}
	return decodeValues(key, row[1:])
}

func decodeValues(key string, values []databasev1.Value) (sharedstate.Entry, error) {
	if len(values) != 3 || values[0].Type != "bytes" || values[1].Type != "int64" || values[2].Type != "timestamp" {
		return sharedstate.Entry{}, errors.New("SQL Shared State entry 类型无效")
	}
	var encoded, revision, updated string
	if json.Unmarshal(values[0].Value, &encoded) != nil || json.Unmarshal(values[1].Value, &revision) != nil || json.Unmarshal(values[2].Value, &updated) != nil {
		return sharedstate.Entry{}, errors.New("SQL Shared State entry 编码无效")
	}
	value, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return sharedstate.Entry{}, err
	}
	revisionNumber, err := strconv.ParseUint(revision, 10, 64)
	if err != nil || revisionNumber == 0 {
		return sharedstate.Entry{}, errors.New("SQL Shared State revision 无效")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return sharedstate.Entry{}, err
	}
	return sharedstate.Entry{Key: key, Value: value, Revision: revisionNumber, UpdatedAt: updatedAt.UTC()}, nil
}
