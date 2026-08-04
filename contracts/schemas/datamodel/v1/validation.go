package datamodelv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed vastplan.data-model.schema.json
var schemaJSON []byte

var schemaState struct {
	sync.Once
	value *jsonschema.Schema
	err   error
}

func Parse(raw []byte) (Model, error) {
	schemaState.Do(func() {
		compiler := jsonschema.NewCompiler()
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
		if err == nil {
			err = compiler.AddResource(SchemaURL, document)
		}
		if err == nil {
			schemaState.value, err = compiler.Compile(SchemaURL)
		}
		schemaState.err = err
	})
	if schemaState.err != nil {
		return Model{}, fmt.Errorf("编译 data.model.v1 Schema: %w", schemaState.err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Model{}, fmt.Errorf("解析 DataModel JSON: %w", err)
	}
	if err := schemaState.value.Validate(instance); err != nil {
		return Model{}, fmt.Errorf("DataModel 不符合 Schema: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var model Model
	if err := decoder.Decode(&model); err != nil {
		return Model{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Model{}, errors.New("DataModel 只能包含一个 JSON 文档")
	}
	if err := Validate(model); err != nil {
		return Model{}, err
	}
	return model, nil
}

func Validate(model Model) error {
	if err := validateStructure(model); err != nil {
		return err
	}
	fields := make(map[string]Field, len(model.Fields))
	columns := make(map[string]struct{}, len(model.Fields))
	for _, field := range model.Fields {
		if _, duplicate := fields[field.ID]; duplicate {
			return fmt.Errorf("DataModel 字段重复: %s", field.ID)
		}
		if _, duplicate := columns[field.Column]; duplicate {
			return fmt.Errorf("DataModel 列名重复: %s", field.Column)
		}
		fields[field.ID] = field
		columns[field.Column] = struct{}{}
	}
	for _, fieldID := range model.PrimaryKey {
		field, ok := fields[fieldID]
		if !ok || field.Nullable {
			return fmt.Errorf("DataModel 主键字段无效或可空: %s", fieldID)
		}
	}
	indexed := map[string]struct{}{}
	for _, fieldID := range model.PrimaryKey {
		indexed[fieldID] = struct{}{}
	}
	for _, index := range model.Indexes {
		for _, fieldID := range index.Fields {
			indexed[fieldID] = struct{}{}
		}
	}
	for _, unique := range model.UniqueConstraints {
		for _, fieldID := range unique.Fields {
			indexed[fieldID] = struct{}{}
		}
	}
	for fieldID := range indexed {
		field := fields[fieldID]
		if field.Type == "string" && field.MaxLength == 0 {
			return fmt.Errorf("被索引的 string 字段 %s 必须声明 maxLength", fieldID)
		}
	}
	seenConstraints := map[string]struct{}{}
	for _, index := range model.Indexes {
		if err := validateFieldSet(index.ID, index.Fields, fields, seenConstraints); err != nil {
			return err
		}
	}
	for _, unique := range model.UniqueConstraints {
		if err := validateFieldSet(unique.ID, unique.Fields, fields, seenConstraints); err != nil {
			return err
		}
	}
	if lock := model.OptimisticLock; lock != nil {
		field, ok := fields[lock.Field]
		if !ok || field.Type != "int64" || field.Nullable {
			return errors.New("optimisticLock 必须引用非空 int64 字段")
		}
	}
	if audit := model.Audit; audit != nil {
		for label, fieldID := range map[string]string{"createdAt": audit.CreatedAt, "updatedAt": audit.UpdatedAt} {
			field, ok := fields[fieldID]
			if !ok || field.Type != "timestamp" || field.Nullable {
				return fmt.Errorf("audit.%s 必须引用非空 timestamp 字段", label)
			}
		}
		for label, fieldID := range map[string]string{"createdBy": audit.CreatedBy, "updatedBy": audit.UpdatedBy} {
			if fieldID == "" {
				continue
			}
			field, ok := fields[fieldID]
			if !ok || field.Type != "string" || field.Nullable {
				return fmt.Errorf("audit.%s 必须引用非空 string 字段", label)
			}
		}
	}
	if model.Deletion.Mode == "soft" {
		field, ok := fields[model.Deletion.Field]
		if !ok || (field.Type != "timestamp" && field.Type != "bool") {
			return errors.New("soft deletion 必须引用 timestamp 或 bool 字段")
		}
	} else if model.Deletion.Field != "" {
		return errors.New("hard deletion 不得声明删除标记字段")
	}
	for scope, mode := range map[string]string{"tenantId": model.Scope.Tenant, "serviceId": model.Scope.Service} {
		if mode != "required" {
			continue
		}
		field, ok := fields[scope]
		if !ok || field.Type != "string" || field.Nullable {
			return fmt.Errorf("required scope 必须声明非空 string 字段 %s", scope)
		}
	}
	return nil
}

func validateFieldSet(id string, values []string, fields map[string]Field, seen map[string]struct{}) error {
	if _, duplicate := seen[id]; duplicate {
		return fmt.Errorf("DataModel 约束 ID 重复: %s", id)
	}
	seen[id] = struct{}{}
	local := map[string]struct{}{}
	for _, fieldID := range values {
		if _, ok := fields[fieldID]; !ok {
			return fmt.Errorf("DataModel 约束 %s 引用未知字段 %s", id, fieldID)
		}
		if _, duplicate := local[fieldID]; duplicate {
			return fmt.Errorf("DataModel 约束 %s 重复字段 %s", id, fieldID)
		}
		local[fieldID] = struct{}{}
	}
	return nil
}
