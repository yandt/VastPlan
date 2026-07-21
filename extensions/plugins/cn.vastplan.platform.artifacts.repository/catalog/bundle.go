package catalog

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

const (
	offlineBundleSchemaVersion = "v1"
	maxOfflineBundleBytes      = int64(8 << 30)
)

type OfflineBundleSource interface {
	ReadWithAttestation(pluginservice.Ref) (pluginservice.Artifact, []byte, []byte, error)
}

type OfflineBundleDestination interface {
	Publish(attestationRaw, packageBytes []byte) (pluginservice.Artifact, error)
}

type OfflineBundle struct {
	Path   string
	Size   int64
	SHA256 string
}

type bundleManifest struct {
	SchemaVersion string           `json:"schemaVersion"`
	LockDigest    string           `json:"lockDigest"`
	TrustSHA256   string           `json:"trustSHA256"`
	Artifacts     []bundleArtifact `json:"artifacts"`
}

type bundleArtifact struct {
	Ref               pluginv1.ArtifactRef `json:"ref"`
	SHA256            string               `json:"sha256"`
	PackagePath       string               `json:"packagePath"`
	AttestationPath   string               `json:"attestationPath"`
	AttestationSHA256 string               `json:"attestationSHA256"`
}

var bundleArtifactPathPattern = regexp.MustCompile(`^artifacts/[a-f0-9]{64}/(?:package\.tar\.gz|attestation\.json)$`)

// CreateOfflineBundle materializes a deterministic gzip tar in a private
// temporary file. The caller owns deletion after serving or moving it.
func CreateOfflineBundle(lock pluginv1.ArtifactLock, trustRaw []byte, source OfflineBundleSource, directory string) (OfflineBundle, error) {
	if source == nil || directory == "" {
		return OfflineBundle{}, errors.New("离线 Bundle 必须配置已验证制品源和临时目录")
	}
	if err := ValidateLock(lock); err != nil {
		return OfflineBundle{}, fmt.Errorf("校验 Bundle 制品锁: %w", err)
	}
	trust, trustStore, err := canonicalTrustDocument(trustRaw)
	if err != nil {
		return OfflineBundle{}, err
	}
	if err := ensurePrivateDirectory(directory); err != nil {
		return OfflineBundle{}, err
	}
	file, err := os.CreateTemp(directory, ".bundle-*.tar.gz")
	if err != nil {
		return OfflineBundle{}, err
	}
	path := file.Name()
	committed := false
	defer func() {
		if !committed {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return OfflineBundle{}, err
	}
	gz := gzip.NewWriter(file)
	gz.Header.ModTime = time.Unix(0, 0).UTC()
	gz.Header.OS = 255
	tw := tar.NewWriter(gz)
	closeWriters := func() error { return errors.Join(tw.Close(), gz.Close()) }

	lockRaw, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		_ = closeWriters()
		return OfflineBundle{}, err
	}
	lockRaw = append(lockRaw, '\n')
	if err := writeBundleEntry(tw, "vastplan.lock.json", lockRaw); err != nil {
		_ = closeWriters()
		return OfflineBundle{}, err
	}
	if err := writeBundleEntry(tw, "trust.json", trust); err != nil {
		_ = closeWriters()
		return OfflineBundle{}, err
	}
	trustDigest := sha256.Sum256(trust)
	manifest := bundleManifest{SchemaVersion: offlineBundleSchemaVersion, LockDigest: lock.Digest, TrustSHA256: hex.EncodeToString(trustDigest[:])}
	var total int64
	for _, item := range lock.Packages {
		artifact, packageBytes, proof, err := source.ReadWithAttestation(item.Ref)
		if err != nil {
			_ = closeWriters()
			return OfflineBundle{}, fmt.Errorf("读取 Bundle 制品 %s: %w", refKey(item.Ref), err)
		}
		if err := validateBundleArtifact(item, artifact, packageBytes, proof, trustStore); err != nil {
			_ = closeWriters()
			return OfflineBundle{}, err
		}
		total += int64(len(packageBytes)) + int64(len(proof))
		if total > maxOfflineBundleBytes {
			_ = closeWriters()
			return OfflineBundle{}, fmt.Errorf("离线 Bundle 制品总量超过 %d 字节上限", maxOfflineBundleBytes)
		}
		base := "artifacts/" + item.SHA256
		packagePath, proofPath := base+"/package.tar.gz", base+"/attestation.json"
		if err := writeBundleEntry(tw, packagePath, packageBytes); err != nil {
			_ = closeWriters()
			return OfflineBundle{}, err
		}
		if err := writeBundleEntry(tw, proofPath, proof); err != nil {
			_ = closeWriters()
			return OfflineBundle{}, err
		}
		proofDigest := sha256.Sum256(proof)
		manifest.Artifacts = append(manifest.Artifacts, bundleArtifact{
			Ref: item.Ref, SHA256: item.SHA256, PackagePath: packagePath, AttestationPath: proofPath,
			AttestationSHA256: hex.EncodeToString(proofDigest[:]),
		})
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = closeWriters()
		return OfflineBundle{}, err
	}
	if err := writeBundleEntry(tw, "bundle.manifest.json", append(manifestRaw, '\n')); err != nil {
		_ = closeWriters()
		return OfflineBundle{}, err
	}
	if err := closeWriters(); err != nil {
		return OfflineBundle{}, err
	}
	if err := file.Sync(); err != nil {
		return OfflineBundle{}, err
	}
	if err := file.Close(); err != nil {
		return OfflineBundle{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return OfflineBundle{}, err
	}
	digest, err := fileSHA256(path)
	if err != nil {
		return OfflineBundle{}, err
	}
	committed = true
	return OfflineBundle{Path: path, Size: info.Size(), SHA256: digest}, nil
}

// ImportOfflineBundle treats the archive as untrusted input, stages it outside
// the repository and publishes each exact artifact through the normal signed
// repository adapter. Partial imports are safe and idempotent; no lock becomes
// active merely because its objects were imported.
func ImportOfflineBundle(bundlePath string, destination OfflineBundleDestination) (pluginv1.ArtifactLock, error) {
	if destination == nil || bundlePath == "" {
		return pluginv1.ArtifactLock{}, errors.New("离线 Bundle 导入必须配置目标仓库和文件")
	}
	staging, err := os.MkdirTemp(filepath.Dir(filepath.Clean(bundlePath)), ".bundle-import-*")
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	defer os.RemoveAll(staging)
	if err := os.Chmod(staging, 0o700); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	if err := extractOfflineBundle(bundlePath, staging); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	lockRaw, err := os.ReadFile(filepath.Join(staging, "vastplan.lock.json"))
	if err != nil {
		return pluginv1.ArtifactLock{}, errors.New("离线 Bundle 缺少 vastplan.lock.json")
	}
	var lock pluginv1.ArtifactLock
	if err := decodeStrict(lockRaw, &lock); err != nil {
		return pluginv1.ArtifactLock{}, fmt.Errorf("解析离线 Bundle 制品锁: %w", err)
	}
	if err := ValidateLock(lock); err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	trustRaw, err := os.ReadFile(filepath.Join(staging, "trust.json"))
	if err != nil {
		return pluginv1.ArtifactLock{}, errors.New("离线 Bundle 缺少 trust.json")
	}
	canonicalTrust, bundleTrust, err := canonicalTrustDocument(trustRaw)
	if err != nil {
		return pluginv1.ArtifactLock{}, err
	}
	if string(canonicalTrust) != string(trustRaw) {
		return pluginv1.ArtifactLock{}, errors.New("离线 Bundle 信任快照不是规范 JSON")
	}
	manifestRaw, err := os.ReadFile(filepath.Join(staging, "bundle.manifest.json"))
	if err != nil {
		return pluginv1.ArtifactLock{}, errors.New("离线 Bundle 缺少 bundle.manifest.json")
	}
	var manifest bundleManifest
	if err := decodeStrict(manifestRaw, &manifest); err != nil {
		return pluginv1.ArtifactLock{}, fmt.Errorf("解析 Bundle manifest: %w", err)
	}
	trustDigest := sha256.Sum256(trustRaw)
	if manifest.SchemaVersion != offlineBundleSchemaVersion || manifest.LockDigest != lock.Digest || manifest.TrustSHA256 != hex.EncodeToString(trustDigest[:]) || len(manifest.Artifacts) != len(lock.Packages) {
		return pluginv1.ArtifactLock{}, errors.New("Bundle manifest 与锁、信任快照或制品数量不一致")
	}
	manifestByID := make(map[string]bundleArtifact, len(manifest.Artifacts))
	for _, item := range manifest.Artifacts {
		if _, duplicate := manifestByID[item.Ref.PluginID]; duplicate {
			return pluginv1.ArtifactLock{}, fmt.Errorf("Bundle manifest 重复插件: %s", item.Ref.PluginID)
		}
		manifestByID[item.Ref.PluginID] = item
	}
	for _, item := range lock.Packages {
		manifestItem, ok := manifestByID[item.Ref.PluginID]
		expectedBase := "artifacts/" + item.SHA256
		if !ok || manifestItem.Ref != item.Ref || manifestItem.SHA256 != item.SHA256 || manifestItem.PackagePath != expectedBase+"/package.tar.gz" || manifestItem.AttestationPath != expectedBase+"/attestation.json" {
			return pluginv1.ArtifactLock{}, fmt.Errorf("Bundle manifest 制品路径与锁不一致: %s", item.Ref.PluginID)
		}
		packageBytes, err := os.ReadFile(filepath.Join(staging, filepath.FromSlash(manifestItem.PackagePath)))
		if err != nil {
			return pluginv1.ArtifactLock{}, fmt.Errorf("Bundle 缺少制品包: %s", item.Ref.PluginID)
		}
		proof, err := os.ReadFile(filepath.Join(staging, filepath.FromSlash(manifestItem.AttestationPath)))
		if err != nil {
			return pluginv1.ArtifactLock{}, fmt.Errorf("Bundle 缺少制品证明: %s", item.Ref.PluginID)
		}
		proofDigest := sha256.Sum256(proof)
		if manifestItem.AttestationSHA256 != hex.EncodeToString(proofDigest[:]) {
			return pluginv1.ArtifactLock{}, fmt.Errorf("Bundle 制品证明摘要不一致: %s", item.Ref.PluginID)
		}
		var attestation pluginservice.Attestation
		if err := decodeStrict(proof, &attestation); err != nil {
			return pluginv1.ArtifactLock{}, err
		}
		if err := validateBundleArtifact(item, attestation.Artifact, packageBytes, proof, bundleTrust); err != nil {
			return pluginv1.ArtifactLock{}, err
		}
		published, err := destination.Publish(proof, packageBytes)
		if err != nil {
			return pluginv1.ArtifactLock{}, fmt.Errorf("通过目标仓库信任强制点导入 %s: %w", item.Ref.PluginID, err)
		}
		if published.PluginID != item.Ref.PluginID || published.Version != item.Ref.Version || published.Channel != item.Ref.Channel || published.SHA256 != item.SHA256 {
			return pluginv1.ArtifactLock{}, fmt.Errorf("目标仓库导入回执与锁不一致: %s", item.Ref.PluginID)
		}
	}
	return lock, nil
}

func extractOfflineBundle(bundlePath, staging string) error {
	file, err := os.Open(filepath.Clean(bundlePath))
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return errors.New("离线 Bundle 不是合法 gzip")
	}
	defer gz.Close()
	reader := tar.NewReader(io.LimitReader(gz, maxOfflineBundleBytes+(32<<20)))
	seen := map[string]struct{}{}
	count := 0
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("解析离线 Bundle tar: %w", err)
		}
		count++
		if count > 2051 || header.Typeflag != tar.TypeReg || header.Name != pathpkg.Clean(header.Name) || strings.HasPrefix(header.Name, "/") || strings.HasPrefix(header.Name, "../") {
			return fmt.Errorf("离线 Bundle 包含非法条目: %s", header.Name)
		}
		if header.Name != "vastplan.lock.json" && header.Name != "trust.json" && header.Name != "bundle.manifest.json" && !bundleArtifactPathPattern.MatchString(header.Name) {
			return fmt.Errorf("离线 Bundle 包含未知条目: %s", header.Name)
		}
		if _, duplicate := seen[header.Name]; duplicate {
			return fmt.Errorf("离线 Bundle 条目重复: %s", header.Name)
		}
		seen[header.Name] = struct{}{}
		limit := int64(2 << 20)
		if strings.HasSuffix(header.Name, "/package.tar.gz") {
			limit = 256 << 20
		}
		if header.Size < 0 || header.Size > limit {
			return fmt.Errorf("离线 Bundle 条目超限: %s", header.Name)
		}
		destination := filepath.Join(staging, filepath.FromSlash(header.Name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		written, copyErr := io.CopyN(out, reader, header.Size)
		closeErr := out.Close()
		if err := errors.Join(copyErr, closeErr); err != nil || written != header.Size {
			return fmt.Errorf("写入 Bundle 暂存条目 %s: %w", header.Name, err)
		}
	}
	return nil
}

func canonicalTrustDocument(raw []byte) ([]byte, *pluginservice.TrustStore, error) {
	var document pluginservice.TrustDocument
	if err := decodeStrict(raw, &document); err != nil {
		return nil, nil, fmt.Errorf("解析 Bundle 信任快照: %w", err)
	}
	trustStore, err := pluginservice.NewTrustStore(document)
	if err != nil {
		return nil, nil, fmt.Errorf("校验 Bundle 信任快照: %w", err)
	}
	sort.Slice(document.Keys, func(i, j int) bool {
		if document.Keys[i].Publisher != document.Keys[j].Publisher {
			return document.Keys[i].Publisher < document.Keys[j].Publisher
		}
		return document.Keys[i].KeyID < document.Keys[j].KeyID
	})
	canonical, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(canonical, '\n'), trustStore, nil
}

func validateBundleArtifact(item pluginv1.ArtifactLockPackage, artifact pluginservice.Artifact, packageBytes, proof []byte, trust *pluginservice.TrustStore) error {
	if artifact.PluginID != item.Ref.PluginID || artifact.Version != item.Ref.Version || artifact.Channel != item.Ref.Channel || artifact.SHA256 != item.SHA256 || artifact.Size != item.Size {
		return fmt.Errorf("Bundle 制品与锁不一致: %s", refKey(item.Ref))
	}
	digest := sha256.Sum256(packageBytes)
	if hex.EncodeToString(digest[:]) != item.SHA256 || int64(len(packageBytes)) != item.Size {
		return fmt.Errorf("Bundle 制品字节与锁不一致: %s", refKey(item.Ref))
	}
	var attestation pluginservice.Attestation
	if err := decodeStrict(proof, &attestation); err != nil {
		return fmt.Errorf("解析 Bundle 制品证明 %s: %w", refKey(item.Ref), err)
	}
	if attestation.Publisher != item.Publisher || attestation.KeyID != item.KeyID || attestation.Artifact.SHA256 != item.SHA256 {
		return fmt.Errorf("Bundle 制品证明与锁不一致: %s", refKey(item.Ref))
	}
	if err := trust.Verify(attestation); err != nil {
		return fmt.Errorf("Bundle 信任快照不接受制品 %s: %w", refKey(item.Ref), err)
	}
	return nil
}

func writeBundleEntry(writer *tar.Writer, name string, content []byte) error {
	header := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), ModTime: time.Unix(0, 0).UTC(), AccessTime: time.Time{}, ChangeTime: time.Time{}, Typeflag: tar.TypeReg}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	written, err := writer.Write(content)
	if err == nil && written != len(content) {
		err = io.ErrShortWrite
	}
	return err
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
