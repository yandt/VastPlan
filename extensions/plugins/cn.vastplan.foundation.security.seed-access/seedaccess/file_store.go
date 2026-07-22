package seedaccess

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

type FileStore struct{ Path string }

func (s FileStore) Load() (State, error) {
	if err := validateStorePath(s.Path); err != nil {
		return State{}, err
	}
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Version: StateVersion, Phase: Uninitialized}, nil
	}
	if err != nil {
		return State{}, err
	}
	info, err := os.Lstat(s.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return State{}, errors.New("Seed Access 状态必须是 owner-only 普通文件")
	}
	var state State
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("解析 Seed Access 状态: %w", err)
	}
	if err := validateState(state); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s FileStore) Update(expectedGeneration uint64, next State) (State, error) {
	if err := validateStorePath(s.Path); err != nil {
		return State{}, err
	}
	directory := filepath.Dir(s.Path)
	lock, err := acquireStateLock(s.Path + ".lock")
	if err != nil {
		return State{}, err
	}
	defer lock.Close()
	current, err := s.Load()
	if err != nil {
		return State{}, err
	}
	if current.Generation == next.Generation && reflect.DeepEqual(current, next) {
		return current, nil
	}
	if current.Generation != expectedGeneration || next.Generation != expectedGeneration+1 {
		return State{}, fmt.Errorf("Seed Access CAS 冲突: expected=%d actual=%d next=%d", expectedGeneration, current.Generation, next.Generation)
	}
	if err := validateState(next); err != nil {
		return State{}, err
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return State{}, err
	}
	file, err := os.CreateTemp(directory, ".seed-access-*")
	if err != nil {
		return State{}, err
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return State{}, err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return State{}, err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return State{}, err
	}
	if err := os.Rename(temporary, s.Path); err != nil {
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

func validateStorePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Ext(path) != ".json" {
		return errors.New("Seed Access Store 必须是规范绝对 JSON 路径")
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("Seed Access Store 目录必须是不可被 group/other 写入的普通目录")
	}
	return nil
}

func validateState(state State) error {
	if state.Version != StateVersion {
		return errors.New("Seed Access 状态版本无效")
	}
	if state.Generation == 0 && state.Phase == Uninitialized {
		return nil
	}
	if state.Generation == 0 || state.Operator == nil || state.Operator.ID == "" || state.UpdatedAt.IsZero() {
		return errors.New("Seed Access 已初始化状态缺少 generation、operator 或 updatedAt")
	}
	switch state.Phase {
	case SeedActive:
	case ProviderConfigured:
		if state.ProviderProfile == nil {
			return errors.New("ProviderConfigured 缺少 Provider Profile")
		}
	case ProviderVerified:
		if state.ProviderProfile == nil || state.ProviderSubject == nil {
			return errors.New("ProviderVerified 缺少 Profile 或 Subject")
		}
	case HandoffReady, EnterpriseActive:
		if state.ProviderProfile == nil || state.ProviderSubject == nil || state.Handoff == nil {
			return errors.New("交接状态缺少锁定证据")
		}
	case RecoveryLease:
		if state.Handoff == nil || state.Recovery == nil {
			return errors.New("RecoveryLease 缺少交接或恢复租约")
		}
	default:
		return fmt.Errorf("未知 Seed Access phase %q", state.Phase)
	}
	return nil
}
