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
	"path/filepath"
	"regexp"
	"sort"
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

type OfflineBundleProvenanceSource interface {
	ReadWithProvenance(pluginservice.Ref) (pluginservice.Artifact, []byte, []byte, []byte, []byte, error)
}

type OfflineBundleProvenanceDestination interface {
	PublishWithProvenance(attestationRaw, packageBytes, provenanceRaw, verificationRaw []byte) (pluginservice.Artifact, error)
}

type OfflineBundleSupplyChainSource interface {
	ReadWithSupplyChain(pluginservice.Ref) (pluginservice.Artifact, []byte, []byte, []byte, []byte, []byte, error)
}

type OfflineBundleSupplyChainDestination interface {
	PublishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, securityAdmissionRaw []byte) (pluginservice.Artifact, error)
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
	Ref                          pluginv1.ArtifactRef `json:"ref"`
	SHA256                       string               `json:"sha256"`
	PackagePath                  string               `json:"packagePath"`
	AttestationPath              string               `json:"attestationPath"`
	AttestationSHA256            string               `json:"attestationSHA256"`
	ProvenancePath               string               `json:"provenancePath,omitempty"`
	ProvenanceSHA256             string               `json:"provenanceSHA256,omitempty"`
	ProvenanceVerificationPath   string               `json:"provenanceVerificationPath,omitempty"`
	ProvenanceVerificationSHA256 string               `json:"provenanceVerificationSHA256,omitempty"`
	SecurityAdmissionPath        string               `json:"securityAdmissionPath,omitempty"`
	SecurityAdmissionSHA256      string               `json:"securityAdmissionSHA256,omitempty"`
}

var bundleArtifactPathPattern = regexp.MustCompile(`^artifacts/[a-f0-9]{64}/(?:package\.tar\.gz|attestation\.json|provenance\.dsse\.json|provenance-verification\.json|security-admission\.json)$`)

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
		var artifact pluginservice.Artifact
		var packageBytes, proof []byte
		var provenanceRaw, verificationRaw, admissionRaw []byte
		if supplyChainSource, ok := source.(OfflineBundleSupplyChainSource); ok {
			artifact, packageBytes, proof, provenanceRaw, verificationRaw, admissionRaw, err = supplyChainSource.ReadWithSupplyChain(item.Ref)
			if err != nil {
				_ = closeWriters()
				return OfflineBundle{}, fmt.Errorf("读取 Bundle 供应链证据 %s: %w", refKey(item.Ref), err)
			}
		} else if provenanceSource, ok := source.(OfflineBundleProvenanceSource); ok {
			artifact, packageBytes, proof, provenanceRaw, verificationRaw, err = provenanceSource.ReadWithProvenance(item.Ref)
			if err != nil {
				_ = closeWriters()
				return OfflineBundle{}, fmt.Errorf("读取 Bundle 来源证明 %s: %w", refKey(item.Ref), err)
			}
		} else {
			if trustStore.ProvenanceEnabled() {
				_ = closeWriters()
				return OfflineBundle{}, errors.New("离线 Bundle 来源不支持导出必需的来源证明")
			}
			if trustStore.AssessmentEnabled() {
				_ = closeWriters()
				return OfflineBundle{}, errors.New("离线 Bundle 来源不支持导出必需的安全准入记录")
			}
			artifact, packageBytes, proof, err = source.ReadWithAttestation(item.Ref)
			if err != nil {
				_ = closeWriters()
				return OfflineBundle{}, fmt.Errorf("读取 Bundle 制品 %s: %w", refKey(item.Ref), err)
			}
		}
		if err := validateBundleArtifact(item, artifact, packageBytes, proof, provenanceRaw, verificationRaw, admissionRaw, trustStore); err != nil {
			_ = closeWriters()
			return OfflineBundle{}, err
		}
		total += int64(len(packageBytes)) + int64(len(proof)) + int64(len(provenanceRaw)) + int64(len(verificationRaw)) + int64(len(admissionRaw))
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
		provenancePath, verificationPath := "", ""
		if len(provenanceRaw) != 0 {
			provenancePath, verificationPath = base+"/provenance.dsse.json", base+"/provenance-verification.json"
			if err := writeBundleEntry(tw, provenancePath, provenanceRaw); err != nil {
				_ = closeWriters()
				return OfflineBundle{}, err
			}
			if err := writeBundleEntry(tw, verificationPath, verificationRaw); err != nil {
				_ = closeWriters()
				return OfflineBundle{}, err
			}
		}
		admissionPath := ""
		if len(admissionRaw) != 0 {
			admissionPath = base + "/security-admission.json"
			if err := writeBundleEntry(tw, admissionPath, admissionRaw); err != nil {
				_ = closeWriters()
				return OfflineBundle{}, err
			}
		}
		proofDigest := sha256.Sum256(proof)
		provenanceDigest, verificationDigest, admissionDigest := sha256.Sum256(provenanceRaw), sha256.Sum256(verificationRaw), sha256.Sum256(admissionRaw)
		manifest.Artifacts = append(manifest.Artifacts, bundleArtifact{
			Ref: item.Ref, SHA256: item.SHA256, PackagePath: packagePath, AttestationPath: proofPath,
			AttestationSHA256: hex.EncodeToString(proofDigest[:]), ProvenancePath: provenancePath, ProvenanceVerificationPath: verificationPath,
			ProvenanceSHA256: digestIfPresent(provenanceRaw, provenanceDigest), ProvenanceVerificationSHA256: digestIfPresent(verificationRaw, verificationDigest),
			SecurityAdmissionPath: admissionPath, SecurityAdmissionSHA256: digestIfPresent(admissionRaw, admissionDigest),
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
	if document.Provenance != nil {
		sort.Strings(document.Provenance.RequiredChannels)
		sort.Slice(document.Provenance.Keys, func(i, j int) bool {
			if document.Provenance.Keys[i].ProviderID != document.Provenance.Keys[j].ProviderID {
				return document.Provenance.Keys[i].ProviderID < document.Provenance.Keys[j].ProviderID
			}
			return document.Provenance.Keys[i].KeyID < document.Provenance.Keys[j].KeyID
		})
		sort.Slice(document.Provenance.Requirements, func(i, j int) bool {
			left, right := document.Provenance.Requirements[i], document.Provenance.Requirements[j]
			if left.ID != right.ID {
				return left.ID < right.ID
			}
			if left.Channel != right.Channel {
				return left.Channel < right.Channel
			}
			if left.Publisher != right.Publisher {
				return left.Publisher < right.Publisher
			}
			return left.PluginPrefix < right.PluginPrefix
		})
		for index := range document.Provenance.Requirements {
			requirement := &document.Provenance.Requirements[index]
			sort.Strings(requirement.ProviderIDs)
			sort.Strings(requirement.BuilderIDs)
			sort.Strings(requirement.BuildTypes)
			sort.Strings(requirement.SourceURIPrefixes)
			sort.Strings(requirement.Issuers)
			sort.Strings(requirement.Workflows)
		}
	}
	if document.Assessment != nil {
		sort.Strings(document.Assessment.RequiredChannels)
		sort.Slice(document.Assessment.Keys, func(i, j int) bool {
			if document.Assessment.Keys[i].ProviderID != document.Assessment.Keys[j].ProviderID {
				return document.Assessment.Keys[i].ProviderID < document.Assessment.Keys[j].ProviderID
			}
			return document.Assessment.Keys[i].KeyID < document.Assessment.Keys[j].KeyID
		})
		sort.Slice(document.Assessment.Requirements, func(i, j int) bool {
			left, right := document.Assessment.Requirements[i], document.Assessment.Requirements[j]
			if left.ID != right.ID {
				return left.ID < right.ID
			}
			if left.Channel != right.Channel {
				return left.Channel < right.Channel
			}
			if left.Publisher != right.Publisher {
				return left.Publisher < right.Publisher
			}
			return left.PluginPrefix < right.PluginPrefix
		})
		for index := range document.Assessment.Requirements {
			requirement := &document.Assessment.Requirements[index]
			sort.Strings(requirement.ProviderIDs)
			sort.Strings(requirement.ScannerIDs)
		}
	}
	canonical, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(canonical, '\n'), trustStore, nil
}

func digestIfPresent(raw []byte, digest [sha256.Size]byte) string {
	if len(raw) == 0 {
		return ""
	}
	return hex.EncodeToString(digest[:])
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
