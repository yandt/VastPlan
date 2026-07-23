package pluginsettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// newTestService keeps workflow recovery tests deterministic without retaining
// the retired plugin-specific File State backend in production code.
func newTestService(stateFile string) (*Service, error) {
	service := New()
	if !filepath.IsAbs(stateFile) || filepath.Clean(stateFile) != stateFile {
		return nil, errors.New("测试状态文件必须是规范绝对路径")
	}
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(stateFile)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > maxStateBytes {
			return nil, errors.New("测试状态文件必须是仅属主可访问且大小受限的普通文件")
		}
		raw, readErr := os.ReadFile(stateFile)
		if readErr != nil {
			return nil, readErr
		}
		if err := json.Unmarshal(raw, &service.state); err != nil {
			return nil, fmt.Errorf("解析测试状态: %w", err)
		}
		if service.state.Tenants == nil {
			service.state.Tenants = map[string]*tenantState{}
		}
		if err := service.validateLoaded(); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	service.testSave = func(state persistedState) error { return saveTestState(stateFile, state) }
	return service, nil
}

func saveTestState(stateFile string, state persistedState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(raw) > maxStateBytes {
		return errors.New("测试状态超过上限")
	}
	temporary, err := os.CreateTemp(filepath.Dir(stateFile), ".plugin-settings-test-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, stateFile)
}
