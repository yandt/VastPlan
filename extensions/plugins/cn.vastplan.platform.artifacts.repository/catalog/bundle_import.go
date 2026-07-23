package catalog

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactprovenance"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

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
		if !ok || manifestItem.Ref != item.Ref || manifestItem.SHA256 != item.SHA256 || manifestItem.PackagePath != expectedBase+"/package.tar.gz" || manifestItem.AttestationPath != expectedBase+"/attestation.json" || !validBundleProvenancePaths(manifestItem, expectedBase) || !validBundleAdmissionPath(manifestItem, expectedBase) {
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
		var provenanceRaw, verificationRaw []byte
		if manifestItem.ProvenancePath != "" {
			provenanceRaw, err = os.ReadFile(filepath.Join(staging, filepath.FromSlash(manifestItem.ProvenancePath)))
			if err != nil {
				return pluginv1.ArtifactLock{}, fmt.Errorf("Bundle 缺少来源证明: %s", item.Ref.PluginID)
			}
			verificationRaw, err = os.ReadFile(filepath.Join(staging, filepath.FromSlash(manifestItem.ProvenanceVerificationPath)))
			if err != nil || digestBytes(provenanceRaw) != manifestItem.ProvenanceSHA256 || digestBytes(verificationRaw) != manifestItem.ProvenanceVerificationSHA256 {
				return pluginv1.ArtifactLock{}, fmt.Errorf("Bundle 来源证明摘要不一致: %s", item.Ref.PluginID)
			}
		}
		var admissionRaw []byte
		if manifestItem.SecurityAdmissionPath != "" {
			admissionRaw, err = os.ReadFile(filepath.Join(staging, filepath.FromSlash(manifestItem.SecurityAdmissionPath)))
			if err != nil || digestBytes(admissionRaw) != manifestItem.SecurityAdmissionSHA256 {
				return pluginv1.ArtifactLock{}, fmt.Errorf("Bundle 安全准入记录摘要不一致: %s", item.Ref.PluginID)
			}
		}
		var attestation pluginservice.Attestation
		if err := decodeStrict(proof, &attestation); err != nil {
			return pluginv1.ArtifactLock{}, err
		}
		if err := validateBundleArtifact(item, attestation.Artifact, packageBytes, proof, provenanceRaw, verificationRaw, admissionRaw, bundleTrust); err != nil {
			return pluginv1.ArtifactLock{}, err
		}
		var published pluginservice.Artifact
		if supplyChainDestination, ok := destination.(OfflineBundleSupplyChainDestination); ok {
			published, err = supplyChainDestination.PublishWithSupplyChain(proof, packageBytes, provenanceRaw, verificationRaw, admissionRaw)
		} else if len(admissionRaw) != 0 {
			return pluginv1.ArtifactLock{}, errors.New("目标仓库不支持导入安全准入记录")
		} else if provenanceDestination, ok := destination.(OfflineBundleProvenanceDestination); ok {
			published, err = provenanceDestination.PublishWithProvenance(proof, packageBytes, provenanceRaw, verificationRaw)
		} else if len(provenanceRaw) != 0 {
			return pluginv1.ArtifactLock{}, errors.New("目标仓库不支持导入来源证明")
		} else {
			published, err = destination.Publish(proof, packageBytes)
		}
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
		if count > 4099 || header.Typeflag != tar.TypeReg || header.Name != pathpkg.Clean(header.Name) || strings.HasPrefix(header.Name, "/") || strings.HasPrefix(header.Name, "../") {
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
		} else if strings.HasSuffix(header.Name, "/provenance-verification.json") {
			limit = artifactprovenance.MaxRecordBytes
		} else if strings.HasSuffix(header.Name, "/security-admission.json") {
			limit = artifactassessment.MaxRecordBytes
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

func validateBundleArtifact(item pluginv1.ArtifactLockPackage, artifact pluginservice.Artifact, packageBytes, proof, provenanceRaw, verificationRaw, admissionRaw []byte, trust *pluginservice.TrustStore) error {
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
	if err := trust.VerifyProof(artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes, Proof: proof, Provenance: provenanceRaw, ProvenanceVerification: verificationRaw, SecurityAdmission: admissionRaw}); err != nil {
		return fmt.Errorf("Bundle 信任快照不接受制品 %s: %w", refKey(item.Ref), err)
	}
	return nil
}

func validBundleAdmissionPath(item bundleArtifact, base string) bool {
	allEmpty := item.SecurityAdmissionPath == "" && item.SecurityAdmissionSHA256 == ""
	allPresent := item.SecurityAdmissionPath == base+"/security-admission.json" && len(item.SecurityAdmissionSHA256) == 64
	return allEmpty || allPresent
}

func validBundleProvenancePaths(item bundleArtifact, base string) bool {
	allEmpty := item.ProvenancePath == "" && item.ProvenanceSHA256 == "" && item.ProvenanceVerificationPath == "" && item.ProvenanceVerificationSHA256 == ""
	allPresent := item.ProvenancePath == base+"/provenance.dsse.json" && item.ProvenanceVerificationPath == base+"/provenance-verification.json" && len(item.ProvenanceSHA256) == 64 && len(item.ProvenanceVerificationSHA256) == 64
	return allEmpty || allPresent
}
