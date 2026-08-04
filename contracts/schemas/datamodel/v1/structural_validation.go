package datamodelv1

import (
	"fmt"
	"regexp"
)

var (
	modelIDPattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]*(?:[._-][A-Za-z0-9]+)*$`)
	tablePattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
)

func validateStructure(model Model) error {
	if model.Contract != "data.model.v1" || !modelIDPattern.MatchString(model.ID) || model.SchemaVersion == 0 {
		return fmt.Errorf("DataModel 身份或 schemaVersion 无效")
	}
	if (model.Storage.Kind != "platform-control" && model.Storage.Kind != "connection-ref") || !tablePattern.MatchString(model.Storage.Table) {
		return fmt.Errorf("DataModel 存储绑定无效")
	}
	if len(model.Fields) == 0 || len(model.PrimaryKey) == 0 {
		return fmt.Errorf("DataModel 必须声明字段和主键")
	}
	validTypes := map[string]struct{}{"string": {}, "uuid": {}, "int64": {}, "float64": {}, "bool": {}, "bytes": {}, "timestamp": {}, "json": {}}
	validSensitivity := map[string]struct{}{"public": {}, "internal": {}, "confidential": {}, "secret": {}}
	for _, field := range model.Fields {
		if !modelIDPattern.MatchString(field.ID) || !tablePattern.MatchString(field.Column) {
			return fmt.Errorf("DataModel 字段身份无效: %s", field.ID)
		}
		if _, ok := validTypes[field.Type]; !ok {
			return fmt.Errorf("DataModel 字段类型无效: %s", field.Type)
		}
		if _, ok := validSensitivity[field.Sensitivity]; !ok {
			return fmt.Errorf("DataModel 字段敏感级别无效: %s", field.Sensitivity)
		}
		if field.MaxLength != 0 && (field.Type != "string" || field.MaxLength < 1 || field.MaxLength > 4096) {
			return fmt.Errorf("DataModel 字段 %s 的 maxLength 只适用于 string", field.ID)
		}
	}
	for name, mode := range map[string]string{"tenant": model.Scope.Tenant, "service": model.Scope.Service} {
		if mode != "none" && mode != "required" {
			return fmt.Errorf("DataModel %s scope 无效: %s", name, mode)
		}
	}
	if model.Deletion.Mode != "hard" && model.Deletion.Mode != "soft" {
		return fmt.Errorf("DataModel 删除策略无效: %s", model.Deletion.Mode)
	}
	return nil
}
