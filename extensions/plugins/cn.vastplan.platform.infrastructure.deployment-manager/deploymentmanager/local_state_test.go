package deploymentmanager

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var testStateFiles sync.Map

func openTestService(path string) (*Service, error) {
	service := New()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(raw, &service.data); err != nil {
			return nil, err
		}
		if service.data.Tenants == nil {
			service.data.Tenants = map[string]*tenantState{}
		}
		if err := service.validateLoaded(); err != nil {
			return nil, err
		}
		if service.recoverInterruptedLocked() {
			if err := saveTestState(path, service.data); err != nil {
				return nil, err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	service.testSave = func(value persisted) error { return saveTestState(path, value) }
	testStateFiles.Store(service, path)
	return service, nil
}

func testStateFile(service *Service) string {
	value, _ := testStateFiles.Load(service)
	return value.(string)
}

func saveTestState(path string, value persisted) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > maxStateBytes {
		return errors.New("测试 Deployment Manager 状态超过上限")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".deployment-manager-test-*")
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
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
