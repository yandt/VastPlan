package recordstore

import (
	"context"
	"encoding/json"
	"fmt"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func ensureSchema(ctx context.Context, session Session, dialect Dialect, entry ModelEntry) error {
	parameters := []databasev1.Value{stringValue(entry.Model.ID)}
	statement := databasev1.Statement{SQL: fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s = %s ORDER BY %s DESC LIMIT 1",
		dialect.Quote("schema_version"), dialect.Quote("model_sha256"), dialect.Quote("vastplan_schema_migrations"),
		dialect.Quote("model_id"), dialect.Placeholder(1), dialect.Quote("schema_version")), Parameters: parameters}
	result, err := session.Query(ctx, statement, 1)
	if err != nil {
		return err
	}
	if len(result.Rows) != 1 || len(result.Rows[0]) != 2 {
		return ErrMigrationNeeded
	}
	var version, digest string
	if result.Rows[0][0].Type != "int64" || json.Unmarshal(result.Rows[0][0].Value, &version) != nil ||
		result.Rows[0][1].Type != "string" || json.Unmarshal(result.Rows[0][1].Value, &digest) != nil {
		return ErrMigrationNeeded
	}
	if version != fmt.Sprintf("%d", entry.Model.SchemaVersion) || digest != entry.Ref.SHA256 {
		return ErrMigrationNeeded
	}
	return nil
}
