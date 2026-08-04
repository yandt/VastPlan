package recordstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func withIdempotency(ctx context.Context, session Session, dialect Dialect, identity ExecutionIdentity,
	key string, request, response any, work func() error) error {
	requestRaw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(requestRaw))
	existing, err := readIdempotency(ctx, session, dialect, identity, key)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.digest != digest {
			return ErrConflict
		}
		return json.Unmarshal(existing.response, response)
	}
	if err := work(); err != nil {
		return err
	}
	responseRaw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	statement := idempotencyInsert(dialect, identity, key, digest, responseRaw)
	result, err := session.Execute(ctx, statement)
	if err != nil {
		if constraintViolation(err) {
			return ErrConflict
		}
		return err
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

type idempotencyRecord struct {
	digest   string
	response json.RawMessage
}

func readIdempotency(ctx context.Context, session Session, dialect Dialect, identity ExecutionIdentity, key string) (*idempotencyRecord, error) {
	fields := []string{"owner_plugin_id", "model_id", "tenant_id", "service_id", "caller_id", "idempotency_key"}
	values := []string{identity.OwnerPluginID, identity.ModelID, identity.TenantID, identity.ServiceID, identity.CallerID, key}
	where, parameters := make([]string, 0, len(fields)), make([]databasev1.Value, 0, len(fields))
	for index, field := range fields {
		parameters = append(parameters, stringValue(values[index]))
		where = append(where, fmt.Sprintf("%s = %s", dialect.Quote(field), dialect.Placeholder(index+1)))
	}
	statement := databasev1.Statement{SQL: fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s", dialect.Quote("operation_digest"), dialect.Quote("response"), dialect.Quote("vastplan_record_idempotency"), joinAnd(where)), Parameters: parameters}
	result, err := session.Query(ctx, statement, 2)
	if err != nil {
		return nil, err
	}
	if len(result.Rows) == 0 {
		return nil, nil
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 2 {
		return nil, ErrConflict
	}
	var digest string
	if result.Rows[0][0].Type != "string" || json.Unmarshal(result.Rows[0][0].Value, &digest) != nil {
		return nil, errors.New("幂等账本 digest 无效")
	}
	response, err := jsonWire(result.Rows[0][1])
	if err != nil {
		return nil, err
	}
	return &idempotencyRecord{digest: digest, response: response}, nil
}

func idempotencyInsert(dialect Dialect, identity ExecutionIdentity, key, digest string, response json.RawMessage) databasev1.Statement {
	columns := []string{"owner_plugin_id", "model_id", "tenant_id", "service_id", "caller_id", "idempotency_key", "operation_digest", "response", "created_at"}
	parameters := []databasev1.Value{
		stringValue(identity.OwnerPluginID), stringValue(identity.ModelID), stringValue(identity.TenantID), stringValue(identity.ServiceID),
		stringValue(identity.CallerID), stringValue(key), stringValue(digest), jsonValue(response), timestampValue(nowUTC()),
	}
	return databasev1.Statement{SQL: fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", dialect.Quote("vastplan_record_idempotency"), quoteAll(dialect, columns), placeholders(dialect, len(columns))), Parameters: parameters}
}

func stringValue(value string) databasev1.Value {
	return databasev1.Value{Type: "string", Value: mustJSON(value)}
}
func jsonValue(value json.RawMessage) databasev1.Value {
	return databasev1.Value{Type: "json", Value: clonePayload(value)}
}
func timestampValue(value time.Time) databasev1.Value {
	return databasev1.Value{Type: "timestamp", Value: mustJSON(value.UTC().Format(time.RFC3339Nano))}
}

func jsonWire(value databasev1.Value) (json.RawMessage, error) {
	if value.Type == "json" && json.Valid(value.Value) {
		return clonePayload(value.Value), nil
	}
	if value.Type == "string" {
		var encoded string
		if err := json.Unmarshal(value.Value, &encoded); err == nil && json.Valid([]byte(encoded)) {
			return json.RawMessage(encoded), nil
		}
	}
	return nil, errors.New("数据库 JSON 值无效")
}

func quoteAll(dialect Dialect, values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = dialect.Quote(value)
	}
	return strings.Join(quoted, ", ")
}

func placeholders(dialect Dialect, count int) string {
	values := make([]string, count)
	for index := range values {
		values[index] = dialect.Placeholder(index + 1)
	}
	return strings.Join(values, ", ")
}

func joinAnd(values []string) string { return strings.Join(values, " AND ") }

var nowUTC = func() time.Time { return time.Now().UTC() }
