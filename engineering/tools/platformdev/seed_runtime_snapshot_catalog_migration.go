package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
)

// seedRuntimeLegacyPlatformProfile is the exact pre-productCapabilities wire
// shape. Keep its field order stable because the legacy binding digest was
// computed from this typed JSON representation.
type seedRuntimeLegacyPlatformProfile struct {
	compositioncommonv1.Document
	Target           compositioncommonv1.Target             `json:"target"`
	ServiceClasses   []string                               `json:"serviceClasses"`
	ServiceBaselines []backendcompositionv1.ServiceBaseline `json:"serviceBaselines"`
	Services         []deploymentv2.ServiceUnit             `json:"services"`
}

type seedRuntimeLegacyBackendCatalog struct {
	compositioncommonv1.Document
	Profiles []seedRuntimeLegacyPlatformProfile            `json:"profiles"`
	Bindings []backendcompositionv1.BackendPlatformBinding `json:"bindings"`
}

func migrateLegacySeedRuntimeCatalogFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return migrateLegacySeedRuntimeCatalog(raw)
}

func migrateLegacySeedRuntimeCatalog(raw []byte) ([]byte, error) {
	if catalog, err := backendcompositionv1.ParseBackendPlatformCatalog(raw); err == nil {
		return marshalSeedRuntimeCatalog(catalog)
	}

	var legacy seedRuntimeLegacyBackendCatalog
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err != nil {
		return nil, fmt.Errorf("解析旧版 Backend Platform Catalog: %w", err)
	}
	if err := ensureSeedRuntimeJSONEOF(decoder); err != nil {
		return nil, err
	}

	current := backendcompositionv1.BackendPlatformCatalog{
		Document: legacy.Document,
		Profiles: make([]backendcompositionv1.PlatformProfile, 0, len(legacy.Profiles)),
		Bindings: append([]backendcompositionv1.BackendPlatformBinding(nil), legacy.Bindings...),
	}
	digests := make(map[string]string, len(legacy.Profiles))
	for _, profile := range legacy.Profiles {
		upgraded, err := backendcompositionv1.ValidatePlatformProfile(backendcompositionv1.PlatformProfile{
			Document: profile.Document, Target: profile.Target,
			ServiceClasses: profile.ServiceClasses, ProductCapabilities: []backendcompositionv1.ProductCapability{},
			ServiceBaselines: profile.ServiceBaselines, Services: profile.Services,
		})
		if err != nil {
			return nil, fmt.Errorf("迁移旧版 Backend Platform Profile %q: %w", profile.ID, err)
		}
		legacyDigest := compositioncommonv1.Digest(profile)
		digests[seedRuntimeProfileRefKey(profile.ID, profile.Revision, legacyDigest)] = upgraded.Digest()
		current.Profiles = append(current.Profiles, upgraded)
	}
	for index := range current.Bindings {
		ref := &current.Bindings[index].PlatformProfile
		upgradedDigest, ok := digests[seedRuntimeProfileRefKey(ref.ID, ref.Revision, ref.Digest)]
		if !ok {
			return nil, fmt.Errorf("旧版 Backend Platform Catalog binding 未精确引用已登记 Profile: tenant=%q deployment=%q", current.Bindings[index].TenantID, current.Bindings[index].DeploymentName)
		}
		ref.Digest = upgradedDigest
	}
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(current)
	if err != nil {
		return nil, fmt.Errorf("验证迁移后的 Backend Platform Catalog: %w", err)
	}
	return marshalSeedRuntimeCatalog(validated)
}

func materializeMigratedSeedRuntimeCatalog(snapshot, runDir string) error {
	raw, err := migrateLegacySeedRuntimeCatalogFile(filepath.Join(snapshot, "backend-platform-catalog.json"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, "backend-platform-catalog.json"), raw, 0o600)
}

func marshalSeedRuntimeCatalog(catalog backendcompositionv1.BackendPlatformCatalog) ([]byte, error) {
	raw, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("编码迁移后的 Backend Platform Catalog: %w", err)
	}
	return append(raw, '\n'), nil
}

func ensureSeedRuntimeJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("旧版 Backend Platform Catalog 包含多个 JSON 文档")
		}
		return fmt.Errorf("读取旧版 Backend Platform Catalog 结尾: %w", err)
	}
	return nil
}

func seedRuntimeProfileRefKey(id string, revision uint64, digest string) string {
	return fmt.Sprintf("%s\x00%d\x00%s", id, revision, digest)
}
