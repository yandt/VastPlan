package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

// writeAPIExposureConfiguration derives the API Contract Catalog only from
// local Seed artifacts whose publisher proof has just been verified. The
// resulting private file is referenced by the service startup snapshot;
// browsers and the API Exposure plugin cannot contribute arbitrary contracts.
func (r *runtime) writeAPIExposureConfiguration(repository *pluginservice.SignedRepository, refs []pluginservice.Ref) error {
	sources := make([]pluginv1.APIContractCatalogSource, 0, len(refs))
	for _, ref := range refs {
		artifact, _, err := repository.ReadMetadataWithAttestation(ref)
		if err != nil {
			return fmt.Errorf("验证 API Contract Catalog 制品 %s: %w", ref.PluginID, err)
		}
		manifest, err := pluginv1.ParseManifest(artifact.Manifest)
		if err != nil {
			return fmt.Errorf("解析 API Contract Catalog 清单 %s: %w", ref.PluginID, err)
		}
		sources = append(sources, pluginv1.APIContractCatalogSource{Manifest: manifest, ArtifactSHA256: artifact.SHA256})
	}
	catalog, err := pluginv1.BuildAPIContractCatalog(1, sources)
	if err != nil {
		return fmt.Errorf("构建可信 API Contract Catalog: %w", err)
	}
	catalogRaw, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.persistentStateRoot(), "api-contract-catalog.json"), append(catalogRaw, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入 API Contract Catalog: %w", err)
	}
	return nil
}
