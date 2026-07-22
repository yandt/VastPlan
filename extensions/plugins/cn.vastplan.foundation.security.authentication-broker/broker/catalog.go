package broker

import (
	"errors"
	"os"
	"path/filepath"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
)

type Catalog interface {
	Load() (authenticationv1.AuthenticationProviderCatalog, error)
}

// FileCatalog is the static/seed adapter. Online management implements the
// same narrow interface and publishes a complete immutable catalog generation.
type FileCatalog struct{ Path string }

func (c FileCatalog) Load() (authenticationv1.AuthenticationProviderCatalog, error) {
	if !filepath.IsAbs(c.Path) || filepath.Clean(c.Path) != c.Path || filepath.Ext(c.Path) != ".json" {
		return authenticationv1.AuthenticationProviderCatalog{}, errors.New("Authentication Provider Catalog 必须是规范绝对 JSON 路径")
	}
	info, err := os.Lstat(c.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return authenticationv1.AuthenticationProviderCatalog{}, errors.New("Authentication Provider Catalog 必须是不可被 group/other 写入的普通文件")
	}
	raw, err := os.ReadFile(c.Path)
	if err != nil {
		return authenticationv1.AuthenticationProviderCatalog{}, err
	}
	return authenticationv1.ParseAuthenticationProviderCatalog(raw)
}

func allowedProviders(catalog authenticationv1.AuthenticationProviderCatalog, tenantID, portalID string) ([]authenticationv1.ProviderCatalogEntry, bool) {
	for _, binding := range catalog.Bindings {
		if binding.TenantID != tenantID || binding.PortalID != portalID {
			continue
		}
		allowed := map[string]struct{}{}
		for _, id := range binding.AllowedProviders {
			allowed[id] = struct{}{}
		}
		providers := make([]authenticationv1.ProviderCatalogEntry, 0, len(allowed))
		for _, provider := range catalog.Providers {
			if _, ok := allowed[provider.Profile.ID]; ok {
				providers = append(providers, provider)
			}
		}
		return providers, true
	}
	return nil, false
}
