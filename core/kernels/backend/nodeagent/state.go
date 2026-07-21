package nodeagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const actualStateVersion = 4

// FileStateStore 将实际态原子写入单个 JSON 文件，既能断点恢复也便于本地审计。
type FileStateStore struct {
	Path string
}

func (s FileStateStore) Load() (ActualState, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyActualState(), nil
	}
	if err != nil {
		return ActualState{}, fmt.Errorf("读取实际态: %w", err)
	}
	state, err := decodeActualState(raw)
	if err != nil {
		return ActualState{}, fmt.Errorf("解析实际态: %w", err)
	}
	return state, nil
}

func (s FileStateStore) Save(state ActualState) error {
	if s.Path == "" {
		return errors.New("实际态文件路径不能为空")
	}
	if err := validateActualState(state); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("创建实际态目录: %w", err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化实际态: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".actual-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		_ = os.Remove(tmp) // Rename 成功后文件已不存在；失败时主写入错误优先。
	}()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

// MemoryStateStore 为单元测试和嵌入式调用提供无磁盘实现。
type MemoryStateStore struct {
	mu    sync.Mutex
	state ActualState
}

func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{state: emptyActualState()}
}

func (s *MemoryStateStore) Load() (ActualState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.state), nil
}

func (s *MemoryStateStore) Save(state ActualState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateActualState(state); err != nil {
		return err
	}
	s.state = cloneState(state)
	return nil
}

func emptyActualState() ActualState {
	return ActualState{Version: actualStateVersion, Units: map[string]UnitState{}}
}

func cloneState(state ActualState) ActualState {
	raw, _ := json.Marshal(state)
	var clone ActualState
	_ = json.Unmarshal(raw, &clone)
	if clone.Units == nil {
		clone.Units = map[string]UnitState{}
	}
	return clone
}

type legacyActualStateV1 struct {
	Version          int                          `json:"version"`
	NodeID           string                       `json:"node_id"`
	ObservedRevision uint64                       `json:"observed_revision"`
	ObservedDigest   string                       `json:"observed_digest"`
	AppliedRevision  uint64                       `json:"applied_revision"`
	Units            map[string]legacyUnitStateV1 `json:"units"`
	Errors           []OperationError             `json:"errors,omitempty"`
	UpdatedAt        time.Time                    `json:"updated_at"`
}

type legacyUnitStateV1 struct {
	Fingerprint     string            `json:"fingerprint"`
	AppliedRevision uint64            `json:"applied_revision"`
	Status          string            `json:"status"`
	Plugins         []InstalledPlugin `json:"plugins"`
	PIDs            []int             `json:"pids,omitempty"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	RestartCount    uint64            `json:"restart_count"`
	LastError       string            `json:"last_error,omitempty"`
}

// decodeActualState 是所有持久化后端共享的版本入口。v1 的自由字符串 status 会在
// 读取时迁移为当前封闭 Phase；写回始终只产生当前版本，避免双写两套语义。
func decodeActualState(raw []byte) (ActualState, error) {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return ActualState{}, err
	}
	switch header.Version {
	case 1:
		var legacy legacyActualStateV1
		if err := json.Unmarshal(raw, &legacy); err != nil {
			return ActualState{}, err
		}
		state := ActualState{
			Version: actualStateVersion, NodeID: legacy.NodeID,
			ObservedRevision: legacy.ObservedRevision, ObservedDigest: legacy.ObservedDigest,
			AppliedRevision: legacy.AppliedRevision, Units: make(map[string]UnitState, len(legacy.Units)),
			Errors: legacy.Errors, UpdatedAt: legacy.UpdatedAt,
		}
		for id, old := range legacy.Units {
			phase, err := legacyPhase(old.Status)
			if err != nil {
				return ActualState{}, fmt.Errorf("unit %s: %w", id, err)
			}
			state.Units[id] = UnitState{
				Fingerprint: old.Fingerprint, AppliedRevision: old.AppliedRevision,
				Phase: phase, PhaseChangedAt: legacy.UpdatedAt, Plugins: old.Plugins,
				PIDs: old.PIDs, StartedAt: old.StartedAt, RestartCount: old.RestartCount,
				LastError: old.LastError,
			}
		}
		return state, validateActualState(state)
	case 2, 3, actualStateVersion:
		var state ActualState
		if err := json.Unmarshal(raw, &state); err != nil {
			return ActualState{}, err
		}
		if state.Units == nil {
			state.Units = map[string]UnitState{}
		}
		state.Version = actualStateVersion
		return state, validateActualState(state)
	default:
		return ActualState{}, fmt.Errorf("不支持的实际态版本 %d", header.Version)
	}
}

func legacyPhase(status string) (UnitPhase, error) {
	switch status {
	case "running":
		return PhaseActive, nil
	case "stopped":
		return PhaseInstalledInactive, nil
	case "degraded":
		return PhaseFailed, nil
	default:
		return "", fmt.Errorf("不支持的 v1 status %q", status)
	}
}

func validateActualState(state ActualState) error {
	if state.Version != actualStateVersion {
		return fmt.Errorf("实际态版本必须为 %d，实际为 %d", actualStateVersion, state.Version)
	}
	if (state.ReferenceTenant == "") != (state.ReferenceOwnerID == "") {
		return errors.New("实际态 Assignment 引用 tenant 与 owner 必须同时存在")
	}
	if (state.ReferencePending || state.ReferenceDesiredRevision != 0 || !state.ReferencePublishedAt.IsZero()) && (state.ReferenceTenant == "" || state.ReferenceGeneration == 0) {
		return errors.New("实际态 Assignment 引用 outbox 缺少 owner 或 generation")
	}
	if (state.BootstrapGeneration == 0) != state.BootstrapPublishedAt.IsZero() {
		return errors.New("实际态 Bootstrap 引用 generation 与发布时间必须同时存在")
	}
	for id, unit := range state.Units {
		if !unit.Phase.Valid() {
			return fmt.Errorf("unit %s 的生命周期状态 %q 非法", id, unit.Phase)
		}
		if unit.Candidate != nil && !unit.Candidate.Phase.Valid() {
			return fmt.Errorf("unit %s 的候选生命周期状态 %q 非法", id, unit.Candidate.Phase)
		}
	}
	return nil
}
