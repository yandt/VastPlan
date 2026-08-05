package broker

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var ErrCatalogNotPublished = errors.New("Authentication Provider Catalog 尚未发布")

type Catalog interface {
	Load() (authenticationv1.AuthenticationProviderCatalog, error)
}

type contextualCatalog interface {
	Bind(context.Context, sdk.Host, *contractv1.CallContext) Catalog
}

func bindCatalog(ctx context.Context, catalog Catalog, host sdk.Host, call *contractv1.CallContext) Catalog {
	if contextual, ok := catalog.(contextualCatalog); ok {
		return contextual.Bind(ctx, host, call)
	}
	return catalog
}

// BootstrapFallbackCatalog falls back only when the durable catalog has never
// been published. Database outages and corrupt Shared State remain fatal and
// must enter the platform recovery flow instead of silently restoring JSON as
// an online truth source.
type BootstrapFallbackCatalog struct {
	Primary   Catalog
	Bootstrap Catalog
}

func (c BootstrapFallbackCatalog) Bind(ctx context.Context, host sdk.Host, call *contractv1.CallContext) Catalog {
	return BootstrapFallbackCatalog{Primary: bindCatalog(ctx, c.Primary, host, call), Bootstrap: bindCatalog(ctx, c.Bootstrap, host, call)}
}

func (c BootstrapFallbackCatalog) Load() (authenticationv1.AuthenticationProviderCatalog, error) {
	if c.Primary == nil || c.Bootstrap == nil {
		return authenticationv1.AuthenticationProviderCatalog{}, errors.New("Authentication Provider Catalog 组合不完整")
	}
	catalog, err := c.Primary.Load()
	if err == nil || !errors.Is(err, ErrCatalogNotPublished) {
		return catalog, err
	}
	return c.Bootstrap.Load()
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
