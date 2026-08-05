package recordstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
)

type MigrationKind string

const (
	MigrationNone     MigrationKind = "none"
	MigrationCreate   MigrationKind = "create"
	MigrationAdditive MigrationKind = "additive"
	MigrationSigned   MigrationKind = "signed"
	MigrationManual   MigrationKind = "manual"
)

type MigrationPlan struct {
	Kind        MigrationKind
	MigrationID string
	Statements  []databasev1.Statement
	Reasons     []string
}

func SignedMigrationPlan(dialect Dialect, entry MigrationEntry) (MigrationPlan, error) {
	if dialect == nil {
		return MigrationPlan{}, errors.New("签名迁移 Dialect 不能为空")
	}
	provider, ok := entry.Migration.Plan(dialect.ProviderID())
	if !ok {
		return MigrationPlan{Kind: MigrationManual, Reasons: []string{"签名迁移不支持当前数据库 Provider"}}, ErrMigrationNeeded
	}
	statements := make([]databasev1.Statement, 0, len(provider.Statements))
	for _, sql := range provider.Statements {
		statement := databasev1.Statement{SQL: sql, Parameters: []databasev1.Value{}}
		if err := databasev1.ValidateStatement(statement); err != nil {
			return MigrationPlan{}, err
		}
		statements = append(statements, statement)
	}
	return MigrationPlan{Kind: MigrationSigned, MigrationID: entry.Migration.ID, Statements: statements}, nil
}

func PlanMigration(dialect Dialect, previous *datamodelv1.Model, next datamodelv1.Model) (MigrationPlan, error) {
	if dialect == nil {
		return MigrationPlan{}, errors.New("Schema migration Dialect 不能为空")
	}
	if err := datamodelv1.Validate(next); err != nil {
		return MigrationPlan{}, err
	}
	if previous == nil {
		statement, err := createTableStatement(dialect, next)
		return MigrationPlan{Kind: MigrationCreate, Statements: []databasev1.Statement{statement}}, err
	}
	if err := datamodelv1.Validate(*previous); err != nil {
		return MigrationPlan{}, fmt.Errorf("现有 DataModel 无效: %w", err)
	}
	if previous.ID != next.ID || previous.Storage != next.Storage || next.SchemaVersion <= previous.SchemaVersion {
		return MigrationPlan{Kind: MigrationManual, Reasons: []string{"模型身份、存储绑定或 schemaVersion 不是安全前进变化"}}, nil
	}
	evolution := datamodelv1.ClassifyEvolution(previous, next)
	if evolution.Kind == datamodelv1.EvolutionNone {
		return MigrationPlan{Kind: MigrationNone}, nil
	}
	if evolution.Kind == datamodelv1.EvolutionManual {
		return MigrationPlan{Kind: MigrationManual, Reasons: evolution.Reasons}, nil
	}
	var statements []databasev1.Statement
	var mysqlClauses []string
	for _, field := range evolution.AddedFields {
		if dialect.ProviderID() == "mysql" {
			mysqlClauses = append(mysqlClauses, fmt.Sprintf("ADD COLUMN %s %s NULL", dialect.Quote(field.Column), sqlType(dialect.ProviderID(), field)))
		} else {
			statements = append(statements, databasev1.Statement{SQL: fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s NULL", dialect.Quote(next.Storage.Table), dialect.Quote(field.Column), sqlType(dialect.ProviderID(), field)), Parameters: []databasev1.Value{}})
		}
	}
	for _, index := range evolution.AddedIndexes {
		if dialect.ProviderID() == "mysql" {
			fields := map[string]datamodelv1.Field{}
			for _, field := range next.Fields {
				fields[field.ID] = field
			}
			mysqlClauses = append(mysqlClauses, fmt.Sprintf("ADD INDEX %s (%s)", dialect.Quote(constraintName(next.Storage.Table, index.ID)), joinColumns(dialect, fields, index.Fields)))
		} else {
			statements = append(statements, createIndexStatement(dialect, next, index))
		}
	}
	if len(mysqlClauses) != 0 {
		statements = []databasev1.Statement{ddl(fmt.Sprintf("ALTER TABLE %s %s", dialect.Quote(next.Storage.Table), strings.Join(mysqlClauses, ", ")))}
	}
	if len(statements) == 0 {
		return MigrationPlan{Kind: MigrationNone}, nil
	}
	return MigrationPlan{Kind: MigrationAdditive, Statements: statements}, nil
}

func createTableStatement(dialect Dialect, model datamodelv1.Model) (databasev1.Statement, error) {
	definitions := make([]string, 0, len(model.Fields)+len(model.UniqueConstraints)+1)
	fields := map[string]datamodelv1.Field{}
	for _, field := range model.Fields {
		fields[field.ID] = field
		nullability := " NOT NULL"
		if field.Nullable {
			nullability = " NULL"
		}
		definitions = append(definitions, fmt.Sprintf("%s %s%s", dialect.Quote(field.Column), sqlType(dialect.ProviderID(), field), nullability))
	}
	definitions = append(definitions, "PRIMARY KEY ("+joinColumns(dialect, fields, model.PrimaryKey)+")")
	for _, unique := range model.UniqueConstraints {
		definitions = append(definitions, fmt.Sprintf("CONSTRAINT %s UNIQUE (%s)", dialect.Quote(constraintName(model.Storage.Table, unique.ID)), joinColumns(dialect, fields, unique.Fields)))
	}
	if dialect.ProviderID() == "mysql" {
		for _, index := range model.Indexes {
			unique := ""
			if index.Unique {
				unique = "UNIQUE "
			}
			definitions = append(definitions, fmt.Sprintf("%sKEY %s (%s)", unique, dialect.Quote(constraintName(model.Storage.Table, index.ID)), joinColumns(dialect, fields, index.Fields)))
		}
	}
	statement := databasev1.Statement{SQL: fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", dialect.Quote(model.Storage.Table), strings.Join(definitions, ", ")), Parameters: []databasev1.Value{}}
	if err := databasev1.ValidateStatement(statement); err != nil {
		return databasev1.Statement{}, err
	}
	return statement, nil
}

func IndexStatements(dialect Dialect, model datamodelv1.Model) []databasev1.Statement {
	if dialect.ProviderID() == "mysql" {
		return []databasev1.Statement{}
	}
	statements := make([]databasev1.Statement, 0, len(model.Indexes))
	for _, index := range model.Indexes {
		statements = append(statements, createIndexStatement(dialect, model, index))
	}
	return statements
}

func createIndexStatement(dialect Dialect, model datamodelv1.Model, index datamodelv1.Index) databasev1.Statement {
	fields := map[string]datamodelv1.Field{}
	for _, field := range model.Fields {
		fields[field.ID] = field
	}
	unique := ""
	if index.Unique {
		unique = "UNIQUE "
	}
	return databasev1.Statement{SQL: fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)", unique, dialect.Quote(constraintName(model.Storage.Table, index.ID)), dialect.Quote(model.Storage.Table), joinColumns(dialect, fields, index.Fields)), Parameters: []databasev1.Value{}}
}

func InternalSchemaStatements(dialect Dialect) []databasev1.Statement {
	if dialect.ProviderID() == "postgresql" {
		return []databasev1.Statement{
			ddl(`CREATE TABLE IF NOT EXISTS "vastplan_schema_migrations" ("model_id" TEXT NOT NULL, "schema_version" BIGINT NOT NULL, "model_sha256" CHAR(64) NOT NULL, "migration_id" TEXT NULL, "model_document" JSONB NOT NULL, "applied_at" TIMESTAMPTZ NOT NULL, PRIMARY KEY ("model_id", "schema_version"))`),
			ddl(`CREATE TABLE IF NOT EXISTS "vastplan_record_idempotency" ("identity_hash" CHAR(64) NOT NULL, "owner_plugin_id" TEXT NOT NULL, "model_id" TEXT NOT NULL, "tenant_id" TEXT NOT NULL, "service_id" TEXT NOT NULL, "caller_id" TEXT NOT NULL, "idempotency_key" TEXT NOT NULL, "operation_digest" CHAR(64) NOT NULL, "response" JSONB NOT NULL, "created_at" TIMESTAMPTZ NOT NULL, PRIMARY KEY ("identity_hash"))`),
			ddl(`CREATE TABLE IF NOT EXISTS "vastplan_record_outbox" ("id" CHAR(36) NOT NULL, "identity_hash" CHAR(64) NOT NULL, "owner_plugin_id" TEXT NOT NULL, "model_id" TEXT NOT NULL, "tenant_id" TEXT NOT NULL, "service_id" TEXT NOT NULL, "topic" TEXT NOT NULL, "payload" JSONB NOT NULL, "idempotency_key" TEXT NOT NULL, "created_at" TIMESTAMPTZ NOT NULL, "published_at" TIMESTAMPTZ NULL, PRIMARY KEY ("id"), UNIQUE ("identity_hash"))`),
		}
	}
	return []databasev1.Statement{
		ddl("CREATE TABLE IF NOT EXISTS `vastplan_schema_migrations` (`model_id` VARCHAR(160) NOT NULL, `schema_version` BIGINT NOT NULL, `model_sha256` CHAR(64) NOT NULL, `migration_id` VARCHAR(160) NULL, `model_document` JSON NOT NULL, `applied_at` DATETIME(6) NOT NULL, PRIMARY KEY (`model_id`, `schema_version`))"),
		ddl("CREATE TABLE IF NOT EXISTS `vastplan_record_idempotency` (`identity_hash` CHAR(64) NOT NULL, `owner_plugin_id` VARCHAR(160) NOT NULL, `model_id` VARCHAR(160) NOT NULL, `tenant_id` VARCHAR(160) NOT NULL, `service_id` VARCHAR(160) NOT NULL, `caller_id` VARCHAR(160) NOT NULL, `idempotency_key` VARCHAR(200) NOT NULL, `operation_digest` CHAR(64) NOT NULL, `response` JSON NOT NULL, `created_at` DATETIME(6) NOT NULL, PRIMARY KEY (`identity_hash`))"),
		ddl("CREATE TABLE IF NOT EXISTS `vastplan_record_outbox` (`id` CHAR(36) NOT NULL, `identity_hash` CHAR(64) NOT NULL, `owner_plugin_id` VARCHAR(160) NOT NULL, `model_id` VARCHAR(160) NOT NULL, `tenant_id` VARCHAR(160) NOT NULL, `service_id` VARCHAR(160) NOT NULL, `topic` VARCHAR(160) NOT NULL, `payload` JSON NOT NULL, `idempotency_key` VARCHAR(200) NOT NULL, `created_at` DATETIME(6) NOT NULL, `published_at` DATETIME(6) NULL, PRIMARY KEY (`id`), UNIQUE KEY `vastplan_outbox_idempotency` (`identity_hash`))"),
	}
}

func ddl(sql string) databasev1.Statement {
	return databasev1.Statement{SQL: sql, Parameters: []databasev1.Value{}}
}

func sqlType(provider string, field datamodelv1.Field) string {
	if provider == "postgresql" {
		return map[string]string{"string": "TEXT", "uuid": "UUID", "int64": "BIGINT", "float64": "DOUBLE PRECISION", "bool": "BOOLEAN", "bytes": "BYTEA", "timestamp": "TIMESTAMPTZ", "json": "JSONB"}[field.Type]
	}
	if field.Type == "string" {
		if field.MaxLength > 0 {
			return fmt.Sprintf("VARCHAR(%d)", field.MaxLength)
		}
		return "TEXT"
	}
	return map[string]string{"uuid": "CHAR(36)", "int64": "BIGINT", "float64": "DOUBLE", "bool": "BOOLEAN", "bytes": "LONGBLOB", "timestamp": "DATETIME(6)", "json": "JSON"}[field.Type]
}

func joinColumns(dialect Dialect, fields map[string]datamodelv1.Field, ids []string) string {
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		values = append(values, dialect.Quote(fields[id].Column))
	}
	return strings.Join(values, ", ")
}

func constraintName(table, id string) string {
	value := strings.ToLower(table + "_" + id)
	value = strings.NewReplacer(".", "_", "-", "_").Replace(value)
	if len(value) > 60 {
		value = value[:60]
	}
	return value
}

func equalStrings(left, right []string) bool {
	return string(mustMarshal(left)) == string(mustMarshal(right))
}
func equalUnique(left, right []datamodelv1.UniqueConstraint) bool {
	return string(mustMarshal(left)) == string(mustMarshal(right))
}
func equalIndex(left, right datamodelv1.Index) bool {
	return string(mustMarshal(left)) == string(mustMarshal(right))
}
func equalLock(left, right *datamodelv1.OptimisticLock) bool {
	return string(mustMarshal(left)) == string(mustMarshal(right))
}
func equalAudit(left, right *datamodelv1.AuditFields) bool {
	return string(mustMarshal(left)) == string(mustMarshal(right))
}
func mustMarshal(value any) []byte { raw, _ := json.Marshal(value); return raw }
