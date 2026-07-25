package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

func (r *runtime) packageArtifacts(ctx context.Context, repository, binDir, nodeBackendModulesDir, frontendModulesDir, dynamicDir string) error {
	specs, err := discoverPackageSpecs(r.options.root)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		args := []string{"run", "./engineering/tools/pluginpackage", "-source", filepath.Join("extensions", "plugins", spec.id), "-repository", repository}
		if spec.backend {
			args = append(args, "-backend-bin", filepath.Join(binDir, spec.id))
		}
		if spec.nodeBackend {
			args = append(args, "-backend-module", filepath.Join(nodeBackendModulesDir, spec.id, filepath.FromSlash(spec.backendEntry)))
		}
		if spec.frontend {
			graphRoot := filepath.Join(frontendModulesDir, spec.id)
			args = append(args, "-frontend-graph", filepath.Join(graphRoot, "frontend", "dist", "vastplan.browser-graph.json"), "-frontend-graph-root", graphRoot)
			if spec.frontendServerEntry != "" {
				args = append(args, "-frontend-server-graph", filepath.Join(graphRoot, "frontend", "dist", "vastplan.server-graph.json"))
			}
		}
		if err := r.command(ctx, map[string]string{"GOCACHE": filepath.Join(r.options.stateRoot, "go-cache")}, "go", args...); err != nil {
			return fmt.Errorf("打包 %s: %w", spec.id, err)
		}
	}
	dynamicPackage, err := os.ReadFile(filepath.Join(dynamicDir, "cn.vastplan.foundation.security.bootstrap-policy.tar.gz"))
	if err != nil {
		return err
	}
	repo, err := pluginservice.NewRepository(repository)
	if err != nil {
		return err
	}
	if _, err := repo.Publish("stable", dynamicPackage); err != nil {
		return fmt.Errorf("发布 bootstrap-policy dynamic-go 制品: %w", err)
	}
	return nil
}

// signPackageRepository upgrades the locally built development repository to a
// signed Seed repository after it has been materialized from the reproducible
// build cache. The signing key is generated per run and is never cached.
func (r *runtime) signPackageRepository() error {
	repository, err := pluginservice.NewRepository(filepath.Join(r.runDir, "repository"))
	if err != nil {
		return err
	}
	trust, err := pluginservice.LoadTrustStore(filepath.Join(r.runDir, "secrets", "seed-artifact-trust.json"))
	if err != nil {
		return err
	}
	privateKey, err := pluginservice.LoadEd25519PrivateKeyPEM(filepath.Join(r.runDir, "secrets", "artifact-signing.pem"))
	if err != nil {
		return err
	}
	refs, err := packageRepositoryRefs(filepath.Join(r.runDir, "repository"))
	if err != nil {
		return err
	}
	signed := &pluginservice.SignedRepository{Local: repository, Trust: trust}
	for _, ref := range refs {
		artifact, packageBytes, err := repository.Read(ref)
		if err != nil {
			return fmt.Errorf("读取待签制品 %s@%s/%s: %w", ref.PluginID, ref.Version, ref.Channel, err)
		}
		manifest, err := pluginv1.ParseManifest(artifact.Manifest)
		if err != nil {
			return fmt.Errorf("解析 %s 的制品清单: %w", ref.PluginID, err)
		}
		attestation, err := pluginservice.SignArtifact(artifact, manifest.Publisher, "local-development", privateKey, time.Now().UTC())
		if err != nil {
			return err
		}
		if _, err := signed.Publish(attestation, packageBytes); err != nil {
			return err
		}
	}
	if r.options.applyPlatform {
		if err := r.writeAPIExposureConfiguration(signed, refs); err != nil {
			return err
		}
		if err := r.writeAuthorizationBootstrap(repository, refs); err != nil {
			return fmt.Errorf("生成开发授权策略: %w", err)
		}
	} else if err := r.writeSessionsFromPublishedAuthorization(); err != nil {
		return fmt.Errorf("恢复已发布开发授权会话: %w", err)
	}
	if err := r.writeBootstrapInventory(repository, refs); err != nil {
		return err
	}
	log.Printf("[6/6] 已签署 %d 个本地 Seed 制品", len(refs))
	return nil
}

func (r *runtime) writeBootstrapInventory(repository *pluginservice.Repository, refs []pluginservice.Ref) error {
	items := make([]bootstrapinventory.Item, 0, len(refs))
	lkgIDs := map[string]struct{}{
		"cn.vastplan.foundation.security.authorization-enforcer":       {},
		"cn.vastplan.foundation.security.platform-admin-access-policy": {},
		"cn.vastplan.platform.artifacts.storage.file":                  {},
		"cn.vastplan.platform.artifacts.repository":                    {},
	}
	lkg := make([]bootstrapinventory.Item, 0, len(lkgIDs))
	for _, ref := range refs {
		artifact, err := repository.ReadMetadata(ref)
		if err != nil {
			return err
		}
		item := bootstrapinventory.Item{Ref: ref, SHA256: artifact.SHA256}
		items = append(items, item)
		if _, ok := lkgIDs[ref.PluginID]; ok {
			lkg = append(lkg, item)
		}
	}
	inventory, err := bootstrapinventory.Normalize(bootstrapinventory.Inventory{
		Version: bootstrapinventory.Version, Generation: uint64(time.Now().UTC().UnixNano()), RepositoryID: "local-seed",
		Seed: items, LastKnownGood: lkg,
	})
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.runDir, "seed-inventory.json"), append(raw, '\n'), 0o600)
}

func packageRepositoryRefs(root string) ([]pluginservice.Ref, error) {
	refs := make([]pluginservice.Ref, 0)
	err := filepath.WalkDir(filepath.Join(root, "artifacts"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "artifact.json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := pluginv1.ValidateArtifactMetadata(raw); err != nil {
			return err
		}
		var artifact pluginservice.Artifact
		if err := json.Unmarshal(raw, &artifact); err != nil {
			return err
		}
		refs = append(refs, pluginservice.Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(refs, func(i, j int) bool {
		left, right := refs[i], refs[j]
		if left.PluginID != right.PluginID {
			return left.PluginID < right.PluginID
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		return left.Channel < right.Channel
	})
	if len(refs) == 0 {
		return nil, errors.New("Seed 制品仓库为空")
	}
	return refs, nil
}

// discoverPackageSpecs makes plugin manifests the sole development-time source
// for every source-packaged backend plus frontend and native-Go build inputs.
// New Node/Python plugins therefore need no platformdev allow-list entry.
func discoverPackageSpecs(root string) ([]packageSpec, error) {
	pluginsRoot := filepath.Join(root, "extensions", "plugins")
	directories, err := os.ReadDir(pluginsRoot)
	if err != nil {
		return nil, fmt.Errorf("读取插件目录: %w", err)
	}
	specs := make([]packageSpec, 0, len(directories))
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		pluginRoot := filepath.Join(pluginsRoot, directory.Name())
		raw, err := os.ReadFile(filepath.Join(pluginRoot, "vastplan.plugin.json"))
		if err != nil {
			return nil, fmt.Errorf("读取插件 %s 清单: %w", directory.Name(), err)
		}
		manifest, err := pluginv1.ParseManifest(raw)
		if err != nil {
			return nil, fmt.Errorf("解析插件 %s 清单: %w", directory.Name(), err)
		}
		if manifest.ID != directory.Name() {
			return nil, fmt.Errorf("插件目录 %s 与清单 id %s 不一致", directory.Name(), manifest.ID)
		}
		frontendEntry := strings.TrimSpace(manifest.Entry["frontend"])
		frontendServerEntry := strings.TrimSpace(manifest.Entry["frontendServer"])
		backendEntry := strings.TrimSpace(manifest.Entry["backend"])
		if frontendEntry != "" && (!strings.HasPrefix(frontendEntry, "frontend/dist/") || strings.Contains(frontendEntry, "..")) {
			return nil, fmt.Errorf("插件 %s entry.frontend 必须位于 frontend/dist/", manifest.ID)
		}
		if frontendServerEntry != "" && (!strings.HasPrefix(frontendServerEntry, "frontend/dist/") || strings.Contains(frontendServerEntry, "..")) {
			return nil, fmt.Errorf("插件 %s entry.frontendServer 必须位于 frontend/dist/", manifest.ID)
		}
		_, backendErr := os.Stat(filepath.Join(pluginRoot, "backend", "main.go"))
		dynamicGo := manifest.Execution != nil && manifest.Execution.Backend != nil && manifest.Execution.Backend.DynamicGo != nil
		backend := backendErr == nil && !dynamicGo
		nodeBackend := manifest.Execution != nil && manifest.Execution.Backend != nil && manifest.Execution.Backend.Driver == "node-worker"
		if backendErr != nil && !errors.Is(backendErr, os.ErrNotExist) {
			return nil, fmt.Errorf("读取插件 %s backend 入口: %w", manifest.ID, backendErr)
		}
		if (backendEntry == "" || dynamicGo) && frontendEntry == "" {
			continue
		}
		specs = append(specs, packageSpec{id: manifest.ID, backend: backend, nodeBackend: nodeBackend, frontend: frontendEntry != "", backendEntry: backendEntry, frontendEntry: frontendEntry, frontendServerEntry: frontendServerEntry})
	}
	return specs, nil
}

func pluginManifestVersion(root, pluginID string) (string, error) {
	path := filepath.Join(root, "extensions", "plugins", pluginID, "vastplan.plugin.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取插件 %s 清单: %w", pluginID, err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		return "", fmt.Errorf("解析插件 %s 清单: %w", pluginID, err)
	}
	if manifest.ID != pluginID {
		return "", fmt.Errorf("插件清单 %s 的 id 是 %s", pluginID, manifest.ID)
	}
	return manifest.Version, nil
}
