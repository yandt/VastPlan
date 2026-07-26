package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

var seedRuntimeSnapshotFiles = []string{"access-profile-catalog.json", "backend-platform-catalog.json", "seed-inventory.json"}

func copySeedRuntimeSnapshotPayload(source, target string) error {
	for _, directory := range []string{"dynamic", "portal-assets"} {
		if err := materializeCachedDirectory(filepath.Join(source, directory), filepath.Join(target, directory)); err != nil {
			return err
		}
	}
	if err := copyUnsignedSeedRepository(filepath.Join(source, "repository"), filepath.Join(target, "repository")); err != nil {
		return err
	}
	for _, name := range seedRuntimeSnapshotFiles {
		if err := copySnapshotFile(filepath.Join(source, name), filepath.Join(target, name)); err != nil {
			return err
		}
	}
	return nil
}

func materializeSeedRuntimeSnapshot(snapshot, runDir string) error {
	for _, directory := range []string{"dynamic", "portal-assets"} {
		if err := materializeCachedDirectory(filepath.Join(snapshot, directory), filepath.Join(runDir, directory)); err != nil {
			return err
		}
	}
	if err := materializeMutableCachedDirectory(filepath.Join(snapshot, "repository"), filepath.Join(runDir, "repository")); err != nil {
		return err
	}
	for _, name := range []string{"access-profile-catalog.json", "backend-platform-catalog.json"} {
		if err := os.Remove(filepath.Join(runDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := copySnapshotFile(filepath.Join(snapshot, name), filepath.Join(runDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func validateSeedRuntimeSnapshot(root string) ([]artifactrepository.Ref, error) {
	marker, err := readSeedRuntimeSnapshotMarker(root)
	if err != nil {
		return nil, err
	}
	digest, err := seedRuntimeTreeDigest(root)
	if err != nil || digest != marker.Digest {
		return nil, errors.New("Seed Runtime 快照内容摘要不匹配")
	}
	for _, path := range []string{"dynamic/backend-kernel", "dynamic/vastplan-go-dynamic-host", "portal-assets/index.html", "portal-assets/assets/portal-kernel.js"} {
		if err := requireSnapshotRegularFile(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			return nil, err
		}
	}
	if _, err := backendcompositionv1.ParseBackendPlatformCatalogFile(filepath.Join(root, "backend-platform-catalog.json")); err != nil {
		return nil, err
	}
	accessRaw, err := os.ReadFile(filepath.Join(root, "access-profile-catalog.json"))
	if err != nil {
		return nil, err
	}
	if _, err := authenticationv1.ParseAccessProfileCatalog(accessRaw); err != nil {
		return nil, err
	}
	inventory, err := bootstrapinventory.ParseFile(filepath.Join(root, "seed-inventory.json"))
	if err != nil {
		return nil, err
	}
	repository, err := artifactrepository.NewRepository(filepath.Join(root, "repository"))
	if err != nil {
		return nil, err
	}
	refs := make([]artifactrepository.Ref, 0, len(inventory.Seed))
	packageDigests := make(map[string]struct{}, len(inventory.Seed))
	for _, item := range inventory.Seed {
		ref := artifactrepository.Ref{PluginID: item.Ref.PluginID, Version: item.Ref.Version, Channel: item.Ref.Channel}
		artifact, _, err := repository.Read(ref)
		if err != nil || artifact.SHA256 != item.SHA256 {
			return nil, fmt.Errorf("Seed Runtime 快照制品与 inventory 不匹配: %s@%s/%s", ref.PluginID, ref.Version, ref.Channel)
		}
		packageDigests[artifact.SHA256] = struct{}{}
		refs = append(refs, ref)
	}
	repositoryRefs, err := packageRepositoryRefs(filepath.Join(root, "repository"))
	if err != nil || len(repositoryRefs) != len(refs) {
		return nil, errors.New("Seed Runtime 快照仓库包含 inventory 之外的制品")
	}
	sortArtifactRefs(refs)
	if err := validateExactSeedRefs("Seed Runtime 快照", refs, repositoryRefs); err != nil {
		return nil, err
	}
	if err := validateUnsignedSeedRepositoryFiles(filepath.Join(root, "repository"), len(refs), packageDigests); err != nil {
		return nil, err
	}
	return refs, nil
}

func validateUnsignedSeedRepositoryFiles(root string, refCount int, packageDigests map[string]struct{}) error {
	fileCount := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("Seed Runtime 快照仓库包含非普通文件: %s", path)
		}
		fileCount++
		if entry.Name() == "artifact.json" {
			return nil
		}
		digest := strings.TrimSuffix(entry.Name(), ".tar.gz")
		if digest == entry.Name() {
			return fmt.Errorf("Seed Runtime 快照仓库包含非白名单文件: %s", path)
		}
		if _, ok := packageDigests[digest]; !ok {
			return fmt.Errorf("Seed Runtime 快照仓库包含未登记包体: %s", path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if fileCount != refCount*2 {
		return fmt.Errorf("Seed Runtime 快照仓库文件数异常: got=%d want=%d", fileCount, refCount*2)
	}
	return nil
}

func seedRuntimeTreeDigest(root string) (string, error) {
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == "." || relative == ".complete.json" || entry.IsDir() {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("Seed Runtime 快照包含非普通文件: %s", relative)
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, relative := range paths {
		path := filepath.Join(root, relative)
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		_, _ = io.WriteString(hash, filepath.ToSlash(relative)+"\x00"+info.Mode().Perm().String()+"\x00")
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			return "", err
		}
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyUnsignedSeedRepository(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." || entry.IsDir() {
			return os.MkdirAll(filepath.Join(target, relative), 0o700)
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("Seed 仓库包含非普通文件: %s", relative)
		}
		if entry.Name() != "artifact.json" && !strings.HasSuffix(entry.Name(), ".tar.gz") {
			return nil
		}
		return copySnapshotFile(path, filepath.Join(target, relative))
	})
}

func copySnapshotFile(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("Seed Runtime 快照源必须是普通文件: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return copyBuildFile(source, target, info.Mode().Perm())
}

func requireSnapshotRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() == 0 {
		return fmt.Errorf("Seed Runtime 快照文件无效: %s", path)
	}
	return nil
}

func validSnapshotDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func writeAtomicOwnerJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".seed-runtime-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func readOwnerOnlyJSONFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("Seed Runtime 控制文件必须是属主私有普通文件: %s", path)
	}
	return os.ReadFile(path)
}
