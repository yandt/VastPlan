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
)

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

	var legacy backendcompositionv1.BackendPlatformCatalog
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&legacy); err != nil {
		return nil, fmt.Errorf("解析旧版 Backend Platform Catalog: %w", err)
	}
	if err := ensureSeedRuntimeJSONEOF(decoder); err != nil {
		return nil, err
	}

	validated, _, err := backendcompositionv1.UpgradeLegacyBackendPlatformCatalog(legacy)
	if err != nil {
		return nil, err
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
