package recordstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

type TrustedScope struct {
	TenantID  string
	ServiceID string
	ActorID   string
}

type compiledModel struct {
	model      datamodelv1.Model
	fields     map[string]datamodelv1.Field
	fieldOrder []string
}

func compileModel(model datamodelv1.Model) compiledModel {
	compiled := compiledModel{model: model, fields: make(map[string]datamodelv1.Field, len(model.Fields)), fieldOrder: make([]string, 0, len(model.Fields))}
	for _, field := range model.Fields {
		compiled.fields[field.ID] = field
		compiled.fieldOrder = append(compiled.fieldOrder, field.ID)
	}
	return compiled
}

func (m compiledModel) prepareCreate(record recordstorev1.Record, scope TrustedScope, now time.Time) (recordstorev1.Record, error) {
	prepared := cloneRecord(record)
	if err := m.applyTrustedScope(prepared, scope); err != nil {
		return nil, err
	}
	if lock := m.model.OptimisticLock; lock != nil {
		if _, exists := prepared[lock.Field]; !exists {
			prepared[lock.Field] = json.RawMessage(`"1"`)
		}
	}
	if audit := m.model.Audit; audit != nil {
		stamp, _ := json.Marshal(now.UTC().Format(time.RFC3339Nano))
		prepared[audit.CreatedAt], prepared[audit.UpdatedAt] = stamp, append(json.RawMessage(nil), stamp...)
		if audit.CreatedBy != "" {
			prepared[audit.CreatedBy], _ = json.Marshal(scope.ActorID)
		}
		if audit.UpdatedBy != "" {
			prepared[audit.UpdatedBy], _ = json.Marshal(scope.ActorID)
		}
	}
	if m.model.Deletion.Mode == "soft" {
		if _, exists := prepared[m.model.Deletion.Field]; !exists {
			prepared[m.model.Deletion.Field] = json.RawMessage("null")
		}
	}
	if err := m.validateRecord(prepared, true); err != nil {
		return nil, err
	}
	return prepared, nil
}

func (m compiledModel) prepareUpdate(values recordstorev1.Record, scope TrustedScope, now time.Time) (recordstorev1.Record, error) {
	prepared := cloneRecord(values)
	for _, fieldID := range m.model.PrimaryKey {
		if _, exists := prepared[fieldID]; exists {
			return nil, fmt.Errorf("主键字段 %s 不可更新", fieldID)
		}
	}
	for _, fieldID := range []string{"tenantId", "serviceId"} {
		if _, exists := prepared[fieldID]; exists {
			return nil, fmt.Errorf("可信 scope 字段 %s 不可更新", fieldID)
		}
	}
	if lock := m.model.OptimisticLock; lock != nil {
		if _, exists := prepared[lock.Field]; exists {
			return nil, errors.New("乐观锁字段由 Record Store 更新")
		}
	}
	if audit := m.model.Audit; audit != nil {
		if _, exists := prepared[audit.CreatedAt]; exists {
			return nil, errors.New("createdAt 不可更新")
		}
		stamp, _ := json.Marshal(now.UTC().Format(time.RFC3339Nano))
		prepared[audit.UpdatedAt] = stamp
		if audit.UpdatedBy != "" {
			prepared[audit.UpdatedBy], _ = json.Marshal(scope.ActorID)
		}
	}
	if len(prepared) == 0 {
		return nil, errors.New("Update 至少包含一个字段")
	}
	return prepared, m.validateRecord(prepared, false)
}

func (m compiledModel) prepareKey(key recordstorev1.Key, scope TrustedScope) (recordstorev1.Key, error) {
	prepared := recordstorev1.Key{}
	for _, fieldID := range m.model.PrimaryKey {
		value, ok := key[fieldID]
		if !ok {
			return nil, fmt.Errorf("缺少主键字段 %s", fieldID)
		}
		prepared[fieldID] = append(json.RawMessage(nil), value...)
	}
	if len(key) != len(m.model.PrimaryKey) {
		return nil, errors.New("Key 只能包含 DataModel 主键")
	}
	if m.model.Scope.Tenant == "required" {
		prepared["tenantId"], _ = json.Marshal(scope.TenantID)
	}
	if m.model.Scope.Service == "required" {
		prepared["serviceId"], _ = json.Marshal(scope.ServiceID)
	}
	for fieldID, value := range prepared {
		if _, err := wireValue(m.fields[fieldID], value); err != nil {
			return nil, err
		}
	}
	return prepared, nil
}

func (m compiledModel) applyTrustedScope(record recordstorev1.Record, scope TrustedScope) error {
	for fieldID, required := range map[string]bool{"tenantId": m.model.Scope.Tenant == "required", "serviceId": m.model.Scope.Service == "required"} {
		if !required {
			continue
		}
		value := scope.TenantID
		if fieldID == "serviceId" {
			value = scope.ServiceID
		}
		if value == "" {
			return fmt.Errorf("缺少可信 %s scope", fieldID)
		}
		encoded, _ := json.Marshal(value)
		if supplied, exists := record[fieldID]; exists && string(supplied) != string(encoded) {
			return fmt.Errorf("不得伪造可信 scope 字段 %s", fieldID)
		}
		record[fieldID] = encoded
	}
	return nil
}

func (m compiledModel) validateRecord(record recordstorev1.Record, requireAll bool) error {
	for fieldID, raw := range record {
		field, exists := m.fields[fieldID]
		if !exists {
			return fmt.Errorf("未知 DataModel 字段 %s", fieldID)
		}
		if _, err := wireValue(field, raw); err != nil {
			return err
		}
	}
	if requireAll {
		for _, field := range m.model.Fields {
			if !field.Nullable {
				if _, exists := record[field.ID]; !exists {
					return fmt.Errorf("缺少非空字段 %s", field.ID)
				}
			}
		}
	}
	return nil
}

func cloneRecord(record recordstorev1.Record) recordstorev1.Record {
	cloned := make(recordstorev1.Record, len(record))
	for key, value := range record {
		cloned[key] = append(json.RawMessage(nil), value...)
	}
	return cloned
}
