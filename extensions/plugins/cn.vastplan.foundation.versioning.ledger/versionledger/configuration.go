package versionledger

import (
	"errors"
	"fmt"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

type StartupConfiguration struct {
	DefaultProvider string                          `json:"defaultProvider"`
	Providers       []ProviderInstanceConfiguration `json:"providers"`
	Routes          []ProviderRoute                 `json:"routes,omitempty"`
}

type ProviderInstanceConfiguration struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	Root     string `json:"root"`
}

type ProviderRoute struct {
	Namespace string `json:"namespace"`
	Provider  string `json:"provider"`
}

func BuildConfiguredService(configuration StartupConfiguration) (*Service, error) {
	registry := NewRegistry()
	for _, instance := range configuration.Providers {
		if instance.Protocol != versioningv1.StorageProtocolFile {
			return nil, fmt.Errorf("P1 尚不支持 Version Provider protocol %q", instance.Protocol)
		}
		provider, err := OpenFileProvider(instance.Root)
		if err != nil {
			return nil, fmt.Errorf("打开 Version Provider %q: %w", instance.ID, err)
		}
		if err := registry.Register(instance.ID, provider); err != nil {
			return nil, err
		}
	}
	return NewService(registry, configuration.DefaultProvider, configuration.Routes)
}

func validateRoutes(registry *Registry, defaultProvider string, routes []ProviderRoute) (map[string]string, error) {
	if registry == nil {
		return nil, errors.New("Version Ledger Registry 不能为空")
	}
	if _, ok := registry.Resolve(defaultProvider); !ok {
		return nil, fmt.Errorf("默认 Version Provider %q 未注册", defaultProvider)
	}
	resolved := make(map[string]string, len(routes))
	for _, route := range routes {
		if err := versioningv1.ValidateStreamKey(versioningv1.StreamKey{Namespace: route.Namespace, StreamID: "route"}); err != nil {
			return nil, fmt.Errorf("Version Provider route namespace: %w", err)
		}
		if _, ok := registry.Resolve(route.Provider); !ok {
			return nil, fmt.Errorf("Version Provider route %q 引用未注册实例 %q", route.Namespace, route.Provider)
		}
		if _, duplicate := resolved[route.Namespace]; duplicate {
			return nil, fmt.Errorf("Version Provider route %q 重复", route.Namespace)
		}
		resolved[route.Namespace] = route.Provider
	}
	return resolved, nil
}
