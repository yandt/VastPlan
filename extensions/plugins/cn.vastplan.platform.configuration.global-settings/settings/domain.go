package settings

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const changeLimit = 512

var (
	ErrVersionConflict = errors.New("设置版本冲突")
	ErrNotFound        = errors.New("设置不存在")
)

type tenantState struct {
	Revision int64              `json:"revision"`
	Values   map[string]setting `json:"values"`
	Changes  []change           `json:"changes"`
}

type setting struct {
	Value     json.RawMessage `json:"value"`
	Version   int64           `json:"version"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type change struct {
	Key       string    `json:"key"`
	Version   int64     `json:"version"`
	Deleted   bool      `json:"deleted"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func emptyTenantState() tenantState {
	return tenantState{Values: map[string]setting{}, Changes: []change{}}
}

func validateTenantState(state tenantState) error {
	if state.Revision < 0 || state.Values == nil || len(state.Changes) > changeLimit {
		return errors.New("全局设置状态无效")
	}
	for key, value := range state.Values {
		if validateKey(key) != nil || value.Version < 1 || value.Version > state.Revision || value.UpdatedAt.IsZero() || !json.Valid(value.Value) {
			return errors.New("全局设置 value 状态无效")
		}
	}
	var previous int64
	for _, item := range state.Changes {
		if validateKey(item.Key) != nil || item.Version < 1 || item.Version > state.Revision || item.Version <= previous || item.UpdatedAt.IsZero() {
			return errors.New("全局设置 change 状态无效")
		}
		previous = item.Version
	}
	return nil
}

func validateKey(key string) error {
	if strings.TrimSpace(key) == "" || len(key) > 320 || strings.ContainsAny(key, "\x00\r\n") {
		return errors.New("设置 key 必须为 1-320 个非空字符")
	}
	return nil
}

func getSetting(state tenantState, key string) (setting, error) {
	if err := validateKey(key); err != nil {
		return setting{}, err
	}
	value, ok := state.Values[key]
	if !ok {
		return setting{}, ErrNotFound
	}
	value.Value = append(json.RawMessage(nil), value.Value...)
	return value, nil
}

func listSettings(state tenantState, prefix string) []map[string]any {
	keys := make([]string, 0, len(state.Values))
	for key := range state.Values {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	items := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		value := state.Values[key]
		items = append(items, map[string]any{"key": key, "value": value.Value, "version": value.Version, "updatedAt": value.UpdatedAt})
	}
	return items
}

func putSetting(state tenantState, key string, value json.RawMessage, ifVersion *int64, now time.Time) (tenantState, setting, error) {
	if err := validateKey(key); err != nil {
		return state, setting{}, err
	}
	if !json.Valid(value) {
		return state, setting{}, errors.New("设置 value 必须是有效 JSON")
	}
	previous, exists := state.Values[key]
	if ifVersion != nil && ((!exists && *ifVersion != 0) || (exists && previous.Version != *ifVersion)) {
		return state, setting{}, ErrVersionConflict
	}
	state.Revision++
	next := setting{Value: append(json.RawMessage(nil), value...), Version: state.Revision, UpdatedAt: now.UTC()}
	state.Values[key] = next
	state.Changes = boundedChanges(append(state.Changes, change{Key: key, Version: next.Version, UpdatedAt: next.UpdatedAt}))
	return state, next, nil
}

func deleteSetting(state tenantState, key string, ifVersion *int64, now time.Time) (tenantState, int64, error) {
	if err := validateKey(key); err != nil {
		return state, 0, err
	}
	previous, exists := state.Values[key]
	if !exists {
		return state, 0, ErrNotFound
	}
	if ifVersion != nil && previous.Version != *ifVersion {
		return state, 0, ErrVersionConflict
	}
	state.Revision++
	delete(state.Values, key)
	state.Changes = boundedChanges(append(state.Changes, change{Key: key, Version: state.Revision, Deleted: true, UpdatedAt: now.UTC()}))
	return state, state.Revision, nil
}

func changesSince(state tenantState, version int64) ([]change, error) {
	if version < 0 {
		return nil, errors.New("version 不能小于 0")
	}
	if len(state.Changes) != 0 && version < state.Changes[0].Version-1 {
		return nil, errors.New("设置变更游标已过期，请重新 list")
	}
	out := make([]change, 0)
	for _, item := range state.Changes {
		if item.Version > version {
			out = append(out, item)
		}
	}
	return out, nil
}

func boundedChanges(changes []change) []change {
	if len(changes) <= changeLimit {
		return changes
	}
	return append([]change(nil), changes[len(changes)-changeLimit:]...)
}
