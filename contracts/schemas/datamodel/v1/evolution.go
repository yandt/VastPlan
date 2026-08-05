package datamodelv1

import (
	"encoding/json"
	"sort"
)

// EvolutionKind is the storage-independent risk classification shared by
// release preview and the provider-specific Schema Controller. It deliberately
// does not contain SQL: a provider still owns physical DDL generation.
type EvolutionKind string

const (
	EvolutionNone     EvolutionKind = "none"
	EvolutionCreate   EvolutionKind = "create"
	EvolutionAdditive EvolutionKind = "additive"
	EvolutionManual   EvolutionKind = "manual"
)

type Evolution struct {
	Kind         EvolutionKind
	AddedFields  []Field
	AddedIndexes []Index
	Reasons      []string
}

// ClassifyEvolution is the single semantic source for safe automatic schema
// evolution. Only a new model, nullable fields and non-unique indexes are
// automatic. Everything that can narrow, rewrite or remove data is manual.
func ClassifyEvolution(previous *Model, next Model) Evolution {
	if previous == nil {
		return Evolution{Kind: EvolutionCreate}
	}
	if previous.ID == next.ID && previous.SchemaVersion == next.SchemaVersion && equalJSON(*previous, next) {
		return Evolution{Kind: EvolutionNone}
	}
	if previous.ID != next.ID || previous.Storage != next.Storage || next.SchemaVersion <= previous.SchemaVersion {
		return Evolution{Kind: EvolutionManual, Reasons: []string{"模型身份、存储绑定或 schemaVersion 不是安全前进变化"}}
	}

	oldFields := make(map[string]Field, len(previous.Fields))
	for _, field := range previous.Fields {
		oldFields[field.ID] = field
	}
	evolution := Evolution{Kind: EvolutionAdditive}
	for _, field := range next.Fields {
		old, exists := oldFields[field.ID]
		if !exists {
			if field.Nullable {
				evolution.AddedFields = append(evolution.AddedFields, field)
			} else {
				evolution.Reasons = append(evolution.Reasons, "新增非空字段 "+field.ID)
			}
			continue
		}
		delete(oldFields, field.ID)
		if old != field {
			evolution.Reasons = append(evolution.Reasons, "字段定义变化 "+field.ID)
		}
	}
	for fieldID := range oldFields {
		evolution.Reasons = append(evolution.Reasons, "删除字段 "+fieldID)
	}
	if !equalJSON(previous.PrimaryKey, next.PrimaryKey) ||
		!equalJSON(previous.UniqueConstraints, next.UniqueConstraints) ||
		previous.Scope != next.Scope || !equalJSON(previous.OptimisticLock, next.OptimisticLock) ||
		!equalJSON(previous.Audit, next.Audit) || previous.Deletion != next.Deletion {
		evolution.Reasons = append(evolution.Reasons, "主键、唯一约束、作用域、乐观锁、审计或删除策略变化")
	}

	oldIndexes := make(map[string]Index, len(previous.Indexes))
	for _, index := range previous.Indexes {
		oldIndexes[index.ID] = index
	}
	for _, index := range next.Indexes {
		old, exists := oldIndexes[index.ID]
		if !exists {
			if index.Unique {
				evolution.Reasons = append(evolution.Reasons, "新增唯一索引 "+index.ID)
			} else {
				evolution.AddedIndexes = append(evolution.AddedIndexes, index)
			}
			continue
		}
		delete(oldIndexes, index.ID)
		if !equalJSON(old, index) {
			evolution.Reasons = append(evolution.Reasons, "索引定义变化 "+index.ID)
		}
	}
	for indexID := range oldIndexes {
		evolution.Reasons = append(evolution.Reasons, "删除索引 "+indexID)
	}
	if len(evolution.Reasons) != 0 {
		sort.Strings(evolution.Reasons)
		evolution.Kind = EvolutionManual
		return evolution
	}
	if len(evolution.AddedFields) == 0 && len(evolution.AddedIndexes) == 0 {
		evolution.Kind = EvolutionNone
	}
	return evolution
}

func equalJSON(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}
