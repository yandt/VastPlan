package authorizationpolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

type Store interface {
	Load() (State, error)
	CompareAndSwap(expected uint64, next State) (State, error)
}

type FileStore struct {
	Path string
	mu   sync.Mutex
}

func (s *FileStore) Load() (State, error) {
	if err := validateStatePath(s.Path); err != nil {
		return State{}, err
	}
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Version: stateVersion, Roles: []RoleRevision{}, Bindings: []BindingRevision{}, Revocations: []authorizationv1.Revocation{}, Audit: []AuditEvent{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	info, err := os.Lstat(s.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return State{}, errors.New("Authorization Policy 状态必须是 owner-only 普通文件")
	}
	var state State
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("解析 Authorization Policy 状态: %w", err)
	}
	if state.Version != stateVersion {
		return State{}, errors.New("Authorization Policy 状态版本无效")
	}
	return state, nil
}

func (s *FileStore) CompareAndSwap(expected uint64, next State) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.Load()
	if err != nil {
		return State{}, err
	}
	if current.Generation != expected || next.Generation != expected+1 || next.Version != stateVersion {
		return State{}, fmt.Errorf("Authorization Policy CAS 冲突: expected=%d actual=%d next=%d", expected, current.Generation, next.Generation)
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return State{}, err
	}
	directory := filepath.Dir(s.Path)
	temporary, err := os.CreateTemp(directory, ".authorization-policy-*")
	if err != nil {
		return State{}, err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return State{}, err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		return State{}, err
	}
	if err := errors.Join(temporary.Sync(), temporary.Close()); err != nil {
		return State{}, err
	}
	if err := os.Rename(temporaryPath, s.Path); err != nil {
		return State{}, err
	}
	committed = true
	dir, err := os.Open(directory)
	if err != nil {
		return State{}, err
	}
	if err := errors.Join(dir.Sync(), dir.Close()); err != nil {
		return State{}, err
	}
	return next, nil
}

func validateStatePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Ext(path) != ".json" {
		return errors.New("Authorization Policy Store 必须是规范绝对 JSON 路径")
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("Authorization Policy Store 目录必须不可被 group/other 写入")
	}
	return nil
}
