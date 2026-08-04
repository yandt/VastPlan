package recordstore

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
)

type SchemaState struct {
	Version  uint64
	SHA256   string
	Document datamodelv1.Model
}

func ReadSchemaState(ctx context.Context, session Session, dialect Dialect, modelID string) (*SchemaState, error) {
	statement := databasev1.Statement{SQL: fmt.Sprintf("SELECT %s, %s, %s FROM %s WHERE %s = %s ORDER BY %s DESC LIMIT 1",
		dialect.Quote("schema_version"), dialect.Quote("model_sha256"), dialect.Quote("model_document"),
		dialect.Quote("vastplan_schema_migrations"), dialect.Quote("model_id"), dialect.Placeholder(1), dialect.Quote("schema_version")),
		Parameters: []databasev1.Value{stringValue(modelID)}}
	result, err := session.Query(ctx, statement, 1)
	if err != nil {
		return nil, err
	}
	if len(result.Rows) == 0 {
		return nil, nil
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 3 {
		return nil, errors.New("Schema migration ledger 响应无效")
	}
	var version, digest string
	if result.Rows[0][0].Type != "int64" || json.Unmarshal(result.Rows[0][0].Value, &version) != nil ||
		result.Rows[0][1].Type != "string" || json.Unmarshal(result.Rows[0][1].Value, &digest) != nil {
		return nil, errors.New("Schema migration ledger 身份无效")
	}
	parsedVersion, err := strconv.ParseUint(version, 10, 64)
	if err != nil {
		return nil, err
	}
	document, err := jsonWire(result.Rows[0][2])
	if err != nil {
		return nil, err
	}
	var model datamodelv1.Model
	if err := json.Unmarshal(document, &model); err != nil || datamodelv1.Validate(model) != nil {
		return nil, errors.New("Schema migration ledger DataModel 无效")
	}
	return &SchemaState{Version: parsedVersion, SHA256: digest, Document: model}, nil
}

func SchemaLedgerInsert(dialect Dialect, entry ModelEntry) (databasev1.Statement, error) {
	document, err := json.Marshal(entry.Model)
	if err != nil {
		return databasev1.Statement{}, err
	}
	columns := []string{"model_id", "schema_version", "model_sha256", "model_document", "applied_at"}
	parameters := []databasev1.Value{
		stringValue(entry.Model.ID), {Type: "int64", Value: mustJSON(fmt.Sprintf("%d", entry.Model.SchemaVersion))},
		stringValue(entry.Ref.SHA256), jsonValue(document), timestampValue(nowUTC()),
	}
	return databasev1.Statement{SQL: fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", dialect.Quote("vastplan_schema_migrations"), quoteAll(dialect, columns), placeholders(dialect, len(columns))), Parameters: parameters}, nil
}

func SchemaLockStatement(dialect Dialect) databasev1.Statement {
	if dialect.ProviderID() == "postgresql" {
		digest := sha256.Sum256([]byte("vastplan.schema-controller.v1"))
		value := int64(binary.BigEndian.Uint64(digest[:8]))
		return databasev1.Statement{SQL: "SELECT pg_advisory_xact_lock(" + dialect.Placeholder(1) + ")", Parameters: []databasev1.Value{{Type: "int64", Value: mustJSON(strconv.FormatInt(value, 10))}}}
	}
	return databasev1.Statement{SQL: "SELECT GET_LOCK(" + dialect.Placeholder(1) + ", " + dialect.Placeholder(2) + ")", Parameters: []databasev1.Value{stringValue("vastplan.schema-controller.v1"), {Type: "int64", Value: mustJSON("30")}}}
}

func SchemaUnlockStatement(dialect Dialect) databasev1.Statement {
	return databasev1.Statement{SQL: "SELECT RELEASE_LOCK(" + dialect.Placeholder(1) + ")", Parameters: []databasev1.Value{stringValue("vastplan.schema-controller.v1")}}
}

func VerifyLockResult(result databasev1.QueryResult) error {
	if len(result.Rows) == 0 {
		// PostgreSQL advisory lock returns one row with a void/null value; some
		// drivers expose it as an empty row. Successful query is sufficient.
		return nil
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) > 1 {
		return errors.New("Schema migration lock 响应无效")
	}
	if len(result.Rows[0]) == 1 && result.Rows[0][0].Type == "int64" {
		var value string
		if json.Unmarshal(result.Rows[0][0].Value, &value) != nil || value != "1" {
			return errors.New("Schema migration lock 未取得")
		}
	}
	return nil
}
