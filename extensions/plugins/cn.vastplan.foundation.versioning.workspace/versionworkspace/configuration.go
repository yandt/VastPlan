package versionworkspace

import (
	"errors"
	"fmt"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

type StartupConfiguration struct {
	Environments []resourcev1.EnvironmentProfile `json:"environments"`
}

func BuildConfiguredService(configuration StartupConfiguration) (*Service, error) {
	if len(configuration.Environments) == 0 {
		return nil, errors.New("Version Workspace 至少需要一个 Environment Profile")
	}
	catalog := NewCatalog()
	if err := catalog.RegisterAdapter(NewJSONAdapter()); err != nil {
		return nil, err
	}
	for _, environment := range configuration.Environments {
		if err := catalog.RegisterEnvironment(environment); err != nil {
			return nil, fmt.Errorf("注册 Version Environment %q: %w", environment.ID, err)
		}
	}
	manager, err := NewManager(catalog, ManagerOptions{})
	if err != nil {
		return nil, err
	}
	return NewService(manager), nil
}
