package sqlsharedstate

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

type stateRow struct {
	key      string
	value    databasev1.Value
	revision databasev1.Value
	updated  databasev1.Value
}

type memorySessions struct{ rows map[string]stateRow }

func (m *memorySessions) Read(ctx context.Context, work func(recordstore.Session) error) error {
	return work((*memorySession)(m))
}
func (m *memorySessions) Write(ctx context.Context, work func(recordstore.Session) error) error {
	return work((*memorySession)(m))
}

type memorySession memorySessions

func (s *memorySession) Query(_ context.Context, statement databasev1.Statement, maxRows int) (databasev1.QueryResult, error) {
	scope := valueText(statement.Parameters[0])
	if strings.Contains(statement.SQL, "ORDER BY") {
		prefix := strings.TrimSuffix(strings.ReplaceAll(valueText(statement.Parameters[1]), `\%`, `%`), "%")
		cursor := valueText(statement.Parameters[2])
		var keys []string
		for physical, row := range s.rows {
			if strings.HasPrefix(physical, scope+"\x00") && strings.HasPrefix(row.key, prefix) && row.key > cursor {
				keys = append(keys, row.key)
			}
		}
		sort.Strings(keys)
		result := databasev1.QueryResult{}
		for _, key := range keys {
			if len(result.Rows) == maxRows {
				break
			}
			row := s.rows[scope+"\x00"+key]
			result.Rows = append(result.Rows, []databasev1.Value{{Type: "string", Value: jsonValue(key)}, row.value, row.revision, row.updated})
		}
		return result, nil
	}
	key := valueText(statement.Parameters[1])
	row, ok := s.rows[scope+"\x00"+key]
	if !ok {
		return databasev1.QueryResult{}, nil
	}
	return databasev1.QueryResult{Rows: [][]databasev1.Value{{row.value, row.revision, row.updated}}}, nil
}

func (s *memorySession) Execute(_ context.Context, statement databasev1.Statement) (databasev1.ExecuteResult, error) {
	switch {
	case strings.HasPrefix(statement.SQL, "INSERT"):
		scope, key := valueText(statement.Parameters[0]), valueText(statement.Parameters[1])
		physical := scope + "\x00" + key
		if _, exists := s.rows[physical]; exists {
			return databasev1.ExecuteResult{}, codedConstraint{}
		}
		s.rows[physical] = stateRow{key: key, value: statement.Parameters[2], revision: statement.Parameters[3], updated: statement.Parameters[4]}
		return databasev1.ExecuteResult{RowsAffected: 1}, nil
	case strings.HasPrefix(statement.SQL, "UPDATE"):
		scope, key, expected := valueText(statement.Parameters[3]), valueText(statement.Parameters[4]), valueText(statement.Parameters[5])
		physical := scope + "\x00" + key
		row, exists := s.rows[physical]
		if !exists || valueText(row.revision) != expected {
			return databasev1.ExecuteResult{}, nil
		}
		s.rows[physical] = stateRow{key: key, value: statement.Parameters[0], revision: statement.Parameters[1], updated: statement.Parameters[2]}
		return databasev1.ExecuteResult{RowsAffected: 1}, nil
	case strings.HasPrefix(statement.SQL, "DELETE"):
		scope, key, expected := valueText(statement.Parameters[0]), valueText(statement.Parameters[1]), valueText(statement.Parameters[2])
		physical := scope + "\x00" + key
		row, exists := s.rows[physical]
		if !exists || valueText(row.revision) != expected {
			return databasev1.ExecuteResult{}, nil
		}
		delete(s.rows, physical)
		return databasev1.ExecuteResult{RowsAffected: 1}, nil
	}
	return databasev1.ExecuteResult{}, nil
}

type codedConstraint struct{}

func (codedConstraint) Error() string       { return "constraint" }
func (codedConstraint) RuntimeCode() string { return databasev1.ErrorConstraintViolation }

func TestSQLStorePreservesCASIsolationAndPagination(t *testing.T) {
	dialect, _ := recordstore.DialectFor("postgresql")
	sessions := &memorySessions{rows: map[string]stateRow{}}
	store, err := NewStore(dialect, sessions)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	scope := sharedstate.Scope{Kind: sharedstate.ScopeTenant, TenantID: "tenant-a", RuntimeScope: "service-a", PluginID: "cn.vastplan.example", Namespace: "settings"}
	created, err := store.Create(context.Background(), scope, "alpha", []byte("one"))
	if err != nil || created.Revision != 1 {
		t.Fatalf("Create 失败: %+v %v", created, err)
	}
	if _, err := store.Create(context.Background(), scope, "alpha", []byte("duplicate")); err != sharedstate.ErrConflict {
		t.Fatalf("重复 Create 应冲突: %v", err)
	}
	updated, err := store.Update(context.Background(), scope, "alpha", []byte("two"), 1)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("Update 失败: %+v %v", updated, err)
	}
	if _, err := store.Update(context.Background(), scope, "alpha", []byte("stale"), 1); err != sharedstate.ErrConflict {
		t.Fatalf("陈旧 CAS 应冲突: %v", err)
	}
	_, _ = store.Create(context.Background(), scope, "alpine", []byte("three"))
	page, err := store.List(context.Background(), scope, "al", 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor != "alpha" {
		t.Fatalf("第一页错误: %+v %v", page, err)
	}
	page, err = store.List(context.Background(), scope, "al", 1, page.NextCursor)
	if err != nil || len(page.Items) != 1 || page.Items[0].Key != "alpine" {
		t.Fatalf("第二页错误: %+v %v", page, err)
	}
	other := scope
	other.TenantID = "tenant-b"
	if _, err := store.Get(context.Background(), other, "alpha"); err != sharedstate.ErrNotFound {
		t.Fatalf("租户必须隔离: %v", err)
	}
	if err := store.Delete(context.Background(), scope, "alpha", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), scope, "alpha"); err != sharedstate.ErrNotFound {
		t.Fatalf("删除后应不存在: %v", err)
	}
	if err := store.Delete(context.Background(), scope, "alpha", 2); err != sharedstate.ErrNotFound {
		t.Fatalf("删除不存在的 key 应返回 not found: %v", err)
	}
}

func TestSQLStoreSchemaIsProviderSpecificAndValueBounded(t *testing.T) {
	postgres, _ := recordstore.DialectFor("postgresql")
	if sql := SchemaStatements(postgres)[0].SQL; !strings.Contains(sql, "BYTEA") || strings.Contains(sql, "tenant_id") {
		t.Fatal(sql)
	}
	mysql, _ := recordstore.DialectFor("mysql")
	if sql := SchemaStatements(mysql)[0].SQL; !strings.Contains(sql, "BINARY(32)") || !strings.Contains(sql, "LONGBLOB") {
		t.Fatal(sql)
	}
	store, _ := NewStore(postgres, &memorySessions{rows: map[string]stateRow{}})
	scope := sharedstate.Scope{Kind: sharedstate.ScopeService, RuntimeScope: "service-a", PluginID: "cn.vastplan.example", Namespace: "settings"}
	if _, err := store.Create(context.Background(), scope, "too-large", make([]byte, sharedstate.MaxValueBytes+1)); err == nil {
		t.Fatal("1 MiB 上限必须保持")
	}
}

func valueText(value databasev1.Value) string {
	var text string
	_ = json.Unmarshal(value.Value, &text)
	if value.Type == "bytes" {
		raw, _ := base64.StdEncoding.DecodeString(text)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	return text
}

func jsonValue(value any) json.RawMessage { raw, _ := json.Marshal(value); return raw }
