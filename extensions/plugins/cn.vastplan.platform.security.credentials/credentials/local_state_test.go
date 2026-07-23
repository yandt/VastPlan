package credentials

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func openTestService(path string, transit Transit) (*Service, error) {
	policy, _ := (Configuration{}).Policy()
	return openTestServiceWithOptions(path, transit, ServiceOptions{Maintenance: policy})
}

func openTestServiceWithOptions(path string, transit Transit, options ServiceOptions) (*Service, error) {
	service, err := NewWithOptions(transit, options)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(raw, &service.data); err != nil {
			return nil, err
		}
		if service.data.Tenants == nil {
			service.data.Tenants = map[string]map[string]Record{}
		}
		if service.data.Managed == nil {
			service.data.Managed = map[string]map[string]ManagedRecord{}
		}
		if service.data.ManagedAudit == nil {
			service.data.ManagedAudit = map[string]managedAuditState{}
		}
		if service.data.ManagedMaintenance == nil {
			service.data.ManagedMaintenance = map[string]ManagedMaintenanceStatus{}
		}
		if err := validateManagedState(service.data.Managed, service.data.ManagedAudit, service.data.ManagedMaintenance); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	service.testSave = func(value persisted) error { return saveTestCredentials(path, value) }
	return service, nil
}

func saveTestCredentials(path string, value persisted) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".credentials-test-*")
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
