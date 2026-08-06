package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const maximumDevelopmentArtifactMetadataBytes = int64(2 << 20)

type developmentArtifactMetadataProjection struct {
	PluginID string          `json:"pluginId"`
	Version  string          `json:"version"`
	Channel  string          `json:"channel"`
	Manifest json.RawMessage `json:"manifest"`
}

type developmentWorkspaceQuarantineResult struct {
	Artifacts      int
	CatalogRebuilt bool
}

type developmentCatalogProjection struct {
	Items []struct {
		Ref struct {
			PluginID string `json:"pluginId"`
			Version  string `json:"version"`
			Channel  string `json:"channel"`
		} `json:"ref"`
	} `json:"items"`
}

// quarantineIncompatibleDevelopmentWorkspaceArtifacts keeps ephemeral
// workspace records from an older Manifest or runtime contribution contract
// out of the active local-test repository. Stable/testing records and malformed
// metadata remain fail-closed.
func quarantineIncompatibleDevelopmentWorkspaceArtifacts(repositoryRoot string) (developmentWorkspaceQuarantineResult, error) {
	artifactsRoot := filepath.Join(repositoryRoot, "artifacts")
	if _, err := os.Lstat(artifactsRoot); os.IsNotExist(err) {
		return developmentWorkspaceQuarantineResult{}, nil
	} else if err != nil {
		return developmentWorkspaceQuarantineResult{}, fmt.Errorf("检查 local-test 制品目录: %w", err)
	}
	candidates := make([]string, 0)
	err := filepath.WalkDir(artifactsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("local-test 制品目录不允许符号链接: %s", path)
		}
		if entry.IsDir() || entry.Name() != "artifact.json" || filepath.Base(filepath.Dir(path)) != "workspace" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() < 1 || info.Size() > maximumDevelopmentArtifactMetadataBytes {
			return fmt.Errorf("workspace 制品元数据大小无效: %s", path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var metadata developmentArtifactMetadataProjection
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return fmt.Errorf("解析 workspace 制品元数据 %s: %w", path, err)
		}
		if metadata.PluginID == "" || metadata.Version == "" || metadata.Channel != "workspace" || len(metadata.Manifest) == 0 {
			return fmt.Errorf("workspace 制品元数据身份无效: %s", path)
		}
		versionDirectory := filepath.Dir(filepath.Dir(path))
		pluginDirectory := filepath.Dir(versionDirectory)
		if filepath.Base(versionDirectory) != metadata.Version || filepath.Base(pluginDirectory) != metadata.PluginID {
			return fmt.Errorf("workspace 制品元数据与目录身份不一致: %s", path)
		}
		manifest, err := pluginv1.ParseManifest(metadata.Manifest)
		if err != nil && !isCurrentManifestSchemaIncompatible(err) {
			return fmt.Errorf("验证 workspace 制品清单 %s: %w", path, err)
		}
		if err == nil {
			// ParseManifest owns the closed JSON shape. Runtime contribution
			// binding is a second contract layer because every executable
			// contribution must map to an exact runtime.provides declaration.
			// Old workspace candidates are disposable and must be quarantined
			// before one stale candidate can prevent the repository from opening.
			if _, err = pluginv1.BackendRuntimeContributions(manifest); err == nil {
				return nil
			}
		}
		candidates = append(candidates, path)
		return nil
	})
	if err != nil {
		return developmentWorkspaceQuarantineResult{}, fmt.Errorf("扫描 local-test workspace 制品: %w", err)
	}
	for _, metadataPath := range candidates {
		raw, err := os.ReadFile(metadataPath)
		if err != nil {
			return developmentWorkspaceQuarantineResult{}, err
		}
		var metadata developmentArtifactMetadataProjection
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return developmentWorkspaceQuarantineResult{}, err
		}
		digest := sha256.Sum256(raw)
		target := filepath.Join(repositoryRoot, "quarantine", "incompatible-manifest", metadata.PluginID, metadata.Version, hex.EncodeToString(digest[:]))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return developmentWorkspaceQuarantineResult{}, err
		}
		if err := os.Rename(filepath.Dir(metadataPath), target); err != nil {
			return developmentWorkspaceQuarantineResult{}, fmt.Errorf("隔离不兼容 workspace 制品 %s: %w", metadata.PluginID+"@"+metadata.Version, err)
		}
	}
	result := developmentWorkspaceQuarantineResult{Artifacts: len(candidates)}
	rebuild, err := developmentCatalogReferencesQuarantine(repositoryRoot)
	if err != nil {
		return developmentWorkspaceQuarantineResult{}, err
	}
	if rebuild {
		if err := rebuildDevelopmentCatalog(repositoryRoot); err != nil {
			return developmentWorkspaceQuarantineResult{}, err
		}
		result.CatalogRebuilt = true
	}
	return result, nil
}

func developmentCatalogReferencesQuarantine(repositoryRoot string) (bool, error) {
	indexPath := filepath.Join(repositoryRoot, "catalog", "index.json")
	raw, err := os.ReadFile(indexPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取 local-test Catalog 快照: %w", err)
	}
	var index developmentCatalogProjection
	if err := json.Unmarshal(raw, &index); err != nil {
		return false, fmt.Errorf("解析 local-test Catalog 快照: %w", err)
	}
	for _, item := range index.Items {
		if item.Ref.Channel != "workspace" || item.Ref.PluginID == "" || item.Ref.Version == "" {
			continue
		}
		active := filepath.Join(repositoryRoot, "artifacts", item.Ref.PluginID, item.Ref.Version, "workspace")
		if _, err := os.Lstat(active); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return false, fmt.Errorf("检查 Catalog workspace 引用: %w", err)
		}
		quarantined, err := filepath.Glob(filepath.Join(repositoryRoot, "quarantine", "incompatible-manifest", item.Ref.PluginID, item.Ref.Version, "*"))
		if err != nil {
			return false, err
		}
		if len(quarantined) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// rebuildDevelopmentCatalog archives the local-test audit projection before
// recreating it from the remaining immutable artifacts. This is deliberately
// limited to the disposable local-test protocol; production journals are
// never rewritten or relaxed.
func rebuildDevelopmentCatalog(repositoryRoot string) error {
	catalogRoot := filepath.Join(repositoryRoot, "catalog")
	indexRaw, err := os.ReadFile(filepath.Join(catalogRoot, "index.json"))
	if err != nil {
		return fmt.Errorf("读取待归档 local-test Catalog: %w", err)
	}
	digest := sha256.Sum256(indexRaw)
	target := filepath.Join(repositoryRoot, "quarantine", "incompatible-manifest-catalog", hex.EncodeToString(digest[:]))
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("local-test Catalog 隔离目标已存在: %s", target)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	if err := os.Rename(catalogRoot, target); err != nil {
		return fmt.Errorf("归档不兼容 local-test Catalog: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(catalogRoot, "journal"), 0o700); err != nil {
		_ = os.Rename(target, catalogRoot)
		return fmt.Errorf("重建 local-test Catalog 目录: %w", err)
	}
	return nil
}
