package nodeagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

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
	var state ActualState
	if err := json.Unmarshal(raw, &state); err != nil {
		return ActualState{}, fmt.Errorf("解析实际态: %w", err)
	}
	if state.Version != 1 {
		return ActualState{}, fmt.Errorf("不支持的实际态版本 %d", state.Version)
	}
	if state.Units == nil {
		state.Units = map[string]UnitState{}
	}
	return state, nil
}

func (s FileStateStore) Save(state ActualState) error {
	if s.Path == "" {
		return errors.New("实际态文件路径不能为空")
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
	s.state = cloneState(state)
	return nil
}

func emptyActualState() ActualState {
	return ActualState{Version: 1, Units: map[string]UnitState{}}
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
