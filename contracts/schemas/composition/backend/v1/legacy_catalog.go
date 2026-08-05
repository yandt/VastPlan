package backendcompositionv1

import (
	"errors"
	"fmt"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
)

// legacyPlatformProfile is the exact wire shape before productCapabilities.
// Field order is part of the historical digest contract.
type legacyPlatformProfile struct {
	compositioncommonv1.Document
	Target           compositioncommonv1.Target `json:"target"`
	ServiceClasses   []string                   `json:"serviceClasses"`
	ServiceBaselines []ServiceBaseline          `json:"serviceBaselines"`
	Services         []deploymentv2.ServiceUnit `json:"services"`
}

type legacyBackendPlatformCatalog struct {
	compositioncommonv1.Document
	Profiles []legacyPlatformProfile  `json:"profiles"`
	Bindings []BackendPlatformBinding `json:"bindings"`
}

// UpgradeLegacyBackendPlatformCatalog validates the exact pre-
// productCapabilities identity and returns the current normalized Catalog plus
// the historical Catalog digest. Callers must first decode the old document
// with unknown-field rejection.
func UpgradeLegacyBackendPlatformCatalog(catalog BackendPlatformCatalog) (BackendPlatformCatalog, string, error) {
	legacy := legacyBackendPlatformCatalog{
		Document: catalog.Document,
		Profiles: make([]legacyPlatformProfile, 0, len(catalog.Profiles)),
		Bindings: append([]BackendPlatformBinding(nil), catalog.Bindings...),
	}
	upgraded := BackendPlatformCatalog{
		Document: catalog.Document,
		Profiles: make([]PlatformProfile, 0, len(catalog.Profiles)),
		Bindings: append([]BackendPlatformBinding(nil), catalog.Bindings...),
	}
	digests := make(map[string]string, len(catalog.Profiles))
	for _, profile := range catalog.Profiles {
		if profile.ProductCapabilities != nil {
			return BackendPlatformCatalog{}, "", errors.New("Backend Platform Catalog 不是缺少 productCapabilities 的旧版结构")
		}
		legacyProfile := legacyPlatformProfile{
			Document: profile.Document, Target: profile.Target, ServiceClasses: profile.ServiceClasses,
			ServiceBaselines: profile.ServiceBaselines, Services: profile.Services,
		}
		currentProfile, err := ValidatePlatformProfile(PlatformProfile{
			Document: profile.Document, Target: profile.Target,
			ServiceClasses: profile.ServiceClasses, ProductCapabilities: []ProductCapability{},
			ServiceBaselines: profile.ServiceBaselines, Services: profile.Services,
		})
		if err != nil {
			return BackendPlatformCatalog{}, "", fmt.Errorf("迁移旧版 Backend Platform Profile %q: %w", profile.ID, err)
		}
		legacyDigest := compositioncommonv1.Digest(legacyProfile)
		key := profileRefKey(profile.ID, profile.Revision, legacyDigest)
		if _, duplicate := digests[key]; duplicate {
			return BackendPlatformCatalog{}, "", fmt.Errorf("旧版 Backend Platform Catalog profile 重复: %q", profile.ID)
		}
		digests[key] = currentProfile.Digest()
		legacy.Profiles = append(legacy.Profiles, legacyProfile)
		upgraded.Profiles = append(upgraded.Profiles, currentProfile)
	}
	for index := range upgraded.Bindings {
		ref := &upgraded.Bindings[index].PlatformProfile
		currentDigest, ok := digests[profileRefKey(ref.ID, ref.Revision, ref.Digest)]
		if !ok {
			return BackendPlatformCatalog{}, "", fmt.Errorf("旧版 Backend Platform Catalog binding 未精确引用已登记 Profile: tenant=%q deployment=%q", upgraded.Bindings[index].TenantID, upgraded.Bindings[index].DeploymentName)
		}
		ref.Digest = currentDigest
	}
	validated, err := ValidateBackendPlatformCatalog(upgraded)
	if err != nil {
		return BackendPlatformCatalog{}, "", fmt.Errorf("验证迁移后的 Backend Platform Catalog: %w", err)
	}
	return validated, compositioncommonv1.Digest(legacy), nil
}
