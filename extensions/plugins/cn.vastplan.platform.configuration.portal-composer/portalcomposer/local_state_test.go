package portalcomposer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func openTestService(path string, catalog Catalog) (*Service, error) {
	service := New(catalog)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(raw, &service.state); err != nil {
			return nil, err
		}
		if service.state.TestBindings == nil {
			service.state.TestBindings = map[string]portalapi.TestTargetBinding{}
		}
		changed := service.recoverInterruptedTestReleases()
		if service.markCurrentReferencesPendingLocked() {
			changed = true
		}
		if changed {
			if err := saveTestComposerState(path, service.state); err != nil {
				return nil, err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	service.testSave = func(value state) error { return saveTestComposerState(path, value) }
	return service, nil
}

func saveTestComposerState(path string, value state) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > maximumComposerState {
		return errors.New("测试 Portal Composer 状态超过上限")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".portal-composer-test-*")
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
