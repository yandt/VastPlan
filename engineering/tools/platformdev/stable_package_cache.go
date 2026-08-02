package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

var errRecordedStablePackageUnavailable = errors.New("已登记 stable 制品缓存缺失")

var stableCachePluginIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)+$`)

func stablePackageCacheRoot(ledgerPath string) string {
	return filepath.Join(filepath.Dir(ledgerPath), "stable-packages", "objects")
}

// hydrateRecordedStablePackages installs already-published exact refs before
// source packaging starts. A stable ref is therefore a repository input, not a
// request to rebuild the same version. Dynamic Go artifacts are deliberately
// excluded because their identity also contains the current host ABI variant.
func hydrateRecordedStablePackages(repositoryRoot, ledgerPath string, refs []artifactrepository.Ref) (map[string]bool, error) {
	ledger, err := loadStablePackageIdentityLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	known := make(map[string]stablePackageIdentity, len(ledger.Artifacts))
	for _, identity := range ledger.Artifacts {
		if identity.Variant == "" {
			known[stableRefKey(stableArtifactRef(identity))] = identity
		}
	}
	repository, err := artifactrepository.NewRepository(repositoryRoot)
	if err != nil {
		return nil, err
	}
	hydrated := make(map[string]bool)
	seenPlugins := make(map[string]struct{}, len(refs))
	cacheRoot := stablePackageCacheRoot(ledgerPath)
	workspaceStateRoot := filepath.Dir(ledgerPath)
	for _, ref := range refs {
		if _, duplicate := seenPlugins[ref.PluginID]; duplicate {
			return nil, fmt.Errorf("Seed 精确引用包含重复插件: %s", ref.PluginID)
		}
		seenPlugins[ref.PluginID] = struct{}{}
		identity, ok := known[stableRefKey(ref)]
		if !ok {
			continue
		}
		packageBytes, err := loadRecordedStablePackage(workspaceStateRoot, cacheRoot, identity)
		if err != nil {
			return nil, err
		}
		published, err := repository.Publish(ref.Channel, packageBytes)
		if err != nil {
			return nil, fmt.Errorf("装入已登记 stable 制品 %s: %w", stablePackageIdentityLabel(identity), err)
		}
		if err := validateStablePackageBytes(identity, published, packageBytes); err != nil {
			return nil, err
		}
		hydrated[ref.PluginID] = true
	}
	return hydrated, nil
}

func cacheCurrentStablePackage(repositoryRoot, cacheRoot string, identity stablePackageIdentity) error {
	repository, err := artifactrepository.NewRepository(repositoryRoot)
	if err != nil {
		return err
	}
	artifact, packageBytes, err := repository.Read(stableArtifactRef(identity))
	if err != nil {
		return fmt.Errorf("读取待归档 stable 制品 %s: %w", stablePackageIdentityLabel(identity), err)
	}
	if err := validateStablePackageBytes(identity, artifact, packageBytes); err != nil {
		return err
	}
	return writeStablePackageCache(cacheRoot, identity, packageBytes)
}

func loadRecordedStablePackage(workspaceStateRoot, cacheRoot string, identity stablePackageIdentity) ([]byte, error) {
	filename := stablePackageCacheObject(cacheRoot, identity.SHA256)
	packageBytes, err := readStablePackageCache(filename, identity)
	if err == nil {
		return packageBytes, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	packageBytes, err = recoverLegacyStablePackage(workspaceStateRoot, identity)
	if err != nil {
		return nil, err
	}
	if err := writeStablePackageCache(cacheRoot, identity, packageBytes); err != nil {
		return nil, err
	}
	return packageBytes, nil
}

func replaceCandidateStablePackage(repositoryRoot string, identity stablePackageIdentity, packageBytes []byte) error {
	if err := validateStablePackageIdentity(identity); err != nil {
		return err
	}
	reference := stableArtifactRef(identity)
	repository, err := artifactrepository.NewRepository(repositoryRoot)
	if err != nil {
		return err
	}
	if _, _, err := repository.Read(reference); err != nil {
		return fmt.Errorf("复用前验证候选 stable 制品 %s: %w", stablePackageIdentityLabel(identity), err)
	}
	directory := filepath.Join(repositoryRoot, "artifacts", identity.PluginID, identity.Version, identity.Channel)
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("移除未晋级的 stable 候选 %s: %w", stablePackageIdentityLabel(identity), err)
	}
	published, err := repository.Publish(identity.Channel, packageBytes)
	if err != nil {
		return fmt.Errorf("复用已登记 stable 制品 %s: %w", stablePackageIdentityLabel(identity), err)
	}
	if err := validateStablePackageBytes(identity, published, packageBytes); err != nil {
		return err
	}
	return nil
}

func recoverLegacyStablePackage(workspaceStateRoot string, identity stablePackageIdentity) ([]byte, error) {
	if err := validateStablePackageIdentity(identity); err != nil {
		return nil, err
	}
	relative := filepath.Join("artifacts", identity.PluginID, identity.Version, identity.Channel, identity.SHA256+".tar.gz")
	patterns := []string{
		filepath.Join(workspaceStateRoot, "dev-platform", "state", "seed-runtime-snapshots", "*", "repository", relative),
		filepath.Join(workspaceStateRoot, "dev-platform", "build-cache", "packages", "*", "repository", relative),
		filepath.Join(workspaceStateRoot, "dev-platform", "runs", "*", "repository", relative),
	}
	candidates := make([]string, 0)
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, matches...)
	}
	sort.Strings(candidates)
	for _, candidate := range candidates {
		packageBytes, err := readStablePackageCache(candidate, identity)
		if err == nil {
			return packageBytes, nil
		}
	}
	return nil, fmt.Errorf("%w: %s sha256=%s；不能从当前源码重建并覆盖旧版本", errRecordedStablePackageUnavailable, stablePackageIdentityLabel(identity), identity.SHA256)
}

func writeStablePackageCache(cacheRoot string, identity stablePackageIdentity, packageBytes []byte) error {
	artifact, err := artifactrepository.Describe(identity.Channel, packageBytes)
	if err != nil {
		return fmt.Errorf("描述 stable 缓存制品: %w", err)
	}
	if err := validateStablePackageBytes(identity, artifact, packageBytes); err != nil {
		return err
	}
	filename := stablePackageCacheObject(cacheRoot, identity.SHA256)
	if existing, err := readStablePackageCache(filename, identity); err == nil {
		if !equalPackageBytes(existing, packageBytes) {
			return fmt.Errorf("stable 缓存对象 %s 内容冲突", identity.SHA256)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".stable-package-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(packageBytes); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, filename); err != nil {
		existing, readErr := readStablePackageCache(filename, identity)
		if readErr == nil {
			if equalPackageBytes(existing, packageBytes) {
				return nil
			}
			return fmt.Errorf("stable 缓存对象 %s 内容冲突", identity.SHA256)
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return err
	}
	committed = true
	directory, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

func readStablePackageCache(filename string, identity stablePackageIdentity) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("stable 缓存对象必须是普通文件: %s", filename)
	}
	packageBytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	artifact, err := artifactrepository.Describe(identity.Channel, packageBytes)
	if err != nil {
		return nil, fmt.Errorf("验证 stable 缓存对象 %s: %w", filename, err)
	}
	if err := validateStablePackageBytes(identity, artifact, packageBytes); err != nil {
		return nil, fmt.Errorf("验证 stable 缓存对象 %s: %w", filename, err)
	}
	return packageBytes, nil
}

func validateStablePackageBytes(identity stablePackageIdentity, artifact artifactrepository.Artifact, packageBytes []byte) error {
	if artifact.PluginID != identity.PluginID || artifact.Version != identity.Version || artifact.Channel != identity.Channel || artifact.SHA256 != identity.SHA256 {
		return fmt.Errorf("stable 缓存对象身份不匹配: want=%s sha256=%s got=%s@%s/%s sha256=%s", stablePackageIdentityLabel(identity), identity.SHA256, artifact.PluginID, artifact.Version, artifact.Channel, artifact.SHA256)
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return err
	}
	variant := ""
	if manifest.Execution != nil && manifest.Execution.Backend != nil && manifest.Execution.Backend.DynamicGo != nil {
		variant = manifest.Execution.Backend.DynamicGo.Fingerprint
	}
	if variant != identity.Variant {
		return fmt.Errorf("stable 缓存对象 dynamic-go variant 不匹配: want=%s got=%s", identity.Variant, variant)
	}
	digest := sha256.Sum256(packageBytes)
	if hex.EncodeToString(digest[:]) != identity.SHA256 {
		return fmt.Errorf("stable 缓存对象 SHA-256 复验失败: %s", identity.SHA256)
	}
	return nil
}

func stableArtifactRef(identity stablePackageIdentity) artifactrepository.Ref {
	return artifactrepository.Ref{PluginID: identity.PluginID, Version: identity.Version, Channel: identity.Channel}
}

func stableRefKey(ref artifactrepository.Ref) string {
	return ref.PluginID + "\x00" + ref.Version + "\x00" + ref.Channel
}

func stablePackageCacheObject(cacheRoot, digest string) string {
	return filepath.Join(cacheRoot, digest[:2], digest+".tar.gz")
}

func equalPackageBytes(left, right []byte) bool {
	return bytes.Equal(left, right)
}
