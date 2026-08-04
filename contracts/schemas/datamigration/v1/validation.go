package datamigrationv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed vastplan.data-migration.schema.json
var schemaJSON []byte

var compiled struct {
	sync.Once
	schema *jsonschema.Schema
	err    error
}

func Parse(raw []byte) (Migration, error) {
	compiled.Do(func() {
		compiler := jsonschema.NewCompiler()
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
		if err == nil {
			err = compiler.AddResource(SchemaURL, document)
		}
		if err == nil {
			compiled.schema, err = compiler.Compile(SchemaURL)
		}
		compiled.err = err
	})
	if compiled.err != nil {
		return Migration{}, fmt.Errorf("编译 DataMigration Schema: %w", compiled.err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Migration{}, fmt.Errorf("解析 DataMigration JSON: %w", err)
	}
	if err := compiled.schema.Validate(instance); err != nil {
		return Migration{}, fmt.Errorf("DataMigration 不符合 Schema: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var migration Migration
	if err := decoder.Decode(&migration); err != nil {
		return Migration{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Migration{}, errors.New("DataMigration 只能包含一个 JSON 文档")
	}
	if err := Validate(migration); err != nil {
		return Migration{}, err
	}
	return migration, nil
}

func Validate(migration Migration) error {
	if migration.Contract != "data.migration.v1" || !migration.RequiresBackup || !migration.RequiresApproval || !migration.RetrySafe || migration.From.SchemaVersion >= migration.To.SchemaVersion {
		return errors.New("DataMigration 身份、版本或治理前置条件无效")
	}
	seen := map[string]struct{}{}
	for _, plan := range migration.Providers {
		if _, duplicate := seen[plan.ProviderID]; duplicate {
			return fmt.Errorf("DataMigration Provider 重复: %s", plan.ProviderID)
		}
		seen[plan.ProviderID] = struct{}{}
		for _, statement := range plan.Statements {
			if err := validateStatement(statement); err != nil {
				return fmt.Errorf("DataMigration %s: %w", plan.ProviderID, err)
			}
		}
	}
	return nil
}

func validateStatement(statement string) error {
	trimmed := strings.TrimSpace(statement)
	lower := strings.ToLower(trimmed)
	if trimmed != statement || strings.ContainsAny(statement, ";\x00") || strings.Contains(lower, "--") || strings.Contains(lower, "/*") || strings.Contains(lower, "*/") {
		return errors.New("迁移 SQL 必须是无注释、无分号的单条语句")
	}
	if strings.Contains(lower, "vastplan_") {
		return errors.New("迁移 SQL 不得修改 Runtime 内部表")
	}
	verb := strings.ToUpper(strings.Fields(trimmed)[0])
	switch verb {
	case "ALTER", "CREATE", "DROP", "INSERT", "UPDATE", "DELETE":
		return nil
	default:
		return fmt.Errorf("迁移 SQL 动词 %s 不在允许集合", verb)
	}
}
