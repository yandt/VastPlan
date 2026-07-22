package main

import (
	"fmt"
	"os"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
)

func materializeAccessProfileCatalog(sourcePath, platformCatalogPath, targetPath string) error {
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("读取 Access Profile Catalog: %w", err)
	}
	access, err := authenticationv1.ParseAccessProfileCatalog(raw)
	if err != nil {
		return fmt.Errorf("校验 Access Profile Catalog: %w", err)
	}
	platform, err := frontendcompositionv1.ParsePortalPlatformCatalogFile(platformCatalogPath)
	if err != nil {
		return fmt.Errorf("校验 Portal Platform Catalog: %w", err)
	}
	profiles := make(map[string]frontendcompositionv1.PlatformProfile, len(platform.Profiles))
	for _, profile := range platform.Profiles {
		profiles[profile.ID] = profile
	}
	for _, profile := range access.Profiles {
		foundation, found := profiles[profile.PlatformProfile.ID]
		if !found || foundation.Revision != profile.PlatformProfile.Revision || foundation.Digest() != profile.PlatformProfile.Digest {
			return fmt.Errorf("Access Profile %s 引用了不存在或摘要不匹配的 Frontend Platform Profile", profile.ID)
		}
	}
	if err := os.WriteFile(targetPath, raw, 0o600); err != nil {
		return fmt.Errorf("写入 Access Profile Catalog: %w", err)
	}
	return nil
}
