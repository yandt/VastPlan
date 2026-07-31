package catalog

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactprovenance"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
)

func entryFrom(artifact artifactrepository.Artifact, attestationRaw []byte) (Entry, error) {
	var attestation artifactrepository.Attestation
	if err := decodeStrict(attestationRaw, &attestation); err != nil {
		return Entry{}, fmt.Errorf("解析制品证明: %w", err)
	}
	if attestation.Artifact.PluginID != artifact.PluginID || attestation.Artifact.Version != artifact.Version ||
		attestation.Artifact.Channel != artifact.Channel || attestation.Artifact.SHA256 != artifact.SHA256 {
		return Entry{}, errors.New("制品证明与制品元数据不一致")
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return Entry{}, err
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		return Entry{}, err
	}
	targetSet := map[string]struct{}{}
	for target := range manifest.Engines {
		targetSet[target] = struct{}{}
	}
	for target := range manifest.Entry {
		targetSet[target] = struct{}{}
	}
	providedSet := map[string]struct{}{}
	for _, contribution := range contributions {
		providedSet[contribution.ID] = struct{}{}
	}
	if manifest.Runtime != nil {
		for _, provided := range manifest.Runtime.Provides {
			providedSet[provided.Capability] = struct{}{}
		}
	}
	providedCapabilities := make([]string, 0, len(providedSet))
	for capability := range providedSet {
		providedCapabilities = append(providedCapabilities, capability)
	}
	sort.Strings(providedCapabilities)
	targets := make([]string, 0, len(targetSet))
	for target := range targetSet {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	namespace := manifest.ID
	if last := strings.LastIndex(namespace, "."); last > 0 {
		namespace = namespace[:last]
	}
	entry := Entry{
		Ref:    pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel},
		SHA256: artifact.SHA256, Size: artifact.Size, Publisher: attestation.Publisher, KeyID: attestation.KeyID,
		SignedAt: attestation.SignedAt.UTC(), Name: manifest.Name, Description: manifest.Description,
		Namespace: namespace, License: manifest.License, Engines: manifest.Engines,
		Dependencies: manifest.Dependencies, CompositionFeatures: compositionFeatures(manifest), Targets: targets,
		Platforms: backendPlatforms(manifest), RuntimeRequires: runtimeRequires(manifest), RuntimeProvides: runtimeProvides(manifest),
		ProvidedCapabilities: providedCapabilities,
	}
	if manifest.SupplyChain != nil && manifest.SupplyChain.SBOM != nil {
		entry.SBOM = &platformadminapi.ArtifactSBOMDeclaration{Format: manifest.SupplyChain.SBOM.Format, SpecVersion: manifest.SupplyChain.SBOM.SpecVersion, SHA256: manifest.SupplyChain.SBOM.SHA256}
	}
	if manifest.SupplyChain != nil && manifest.SupplyChain.PythonLock != nil {
		entry.PythonLock = &platformadminapi.ArtifactPythonLockDeclaration{Format: manifest.SupplyChain.PythonLock.Format, SpecVersion: manifest.SupplyChain.PythonLock.SpecVersion, SHA256: manifest.SupplyChain.PythonLock.SHA256}
	}
	return entry, nil
}

func compositionFeatures(manifest pluginv1.Manifest) map[string]pluginv1.CompositionFeature {
	if manifest.Composition == nil || len(manifest.Composition.Features) == 0 {
		return nil
	}
	result := make(map[string]pluginv1.CompositionFeature, len(manifest.Composition.Features))
	for _, feature := range manifest.Composition.Features {
		feature.Dependencies = cloneStringMap(feature.Dependencies)
		feature.RuntimeRequires = append([]pluginv1.RuntimeRequirement(nil), feature.RuntimeRequires...)
		feature.ConfigurationSchema = append([]byte(nil), feature.ConfigurationSchema...)
		result[feature.ID] = feature
	}
	return result
}

func (s *Store) enrichProvenance(entry *Entry) error {
	reader, ok := s.repository.(VerifiedProvenanceReader)
	if !ok {
		return nil
	}
	provenanceRaw, verificationRaw, err := reader.ReadProvenance(entry.Ref)
	if err != nil {
		return err
	}
	return populateProvenance(entry, provenanceRaw, verificationRaw)
}

func populateProvenance(entry *Entry, provenanceRaw, verificationRaw []byte) error {
	if len(provenanceRaw) == 0 && len(verificationRaw) == 0 {
		return nil
	}
	record, verificationSHA, err := artifactprovenance.InspectVerificationRecord(verificationRaw)
	if err != nil {
		return err
	}
	_, provenanceSHA, err := artifactprovenance.InspectDSSE(provenanceRaw, entry.SHA256)
	if err != nil || record.SubjectSHA256 != entry.SHA256 || record.ProvenanceSHA256 != provenanceSHA {
		return errors.New("来源证明 sidecar 摘要或 subject 不一致")
	}
	entry.Provenance = &platformadminapi.ArtifactProvenanceDeclaration{
		ProvenanceSHA256: provenanceSHA, VerificationSHA256: verificationSHA,
		PredicateType: record.PredicateType, BuilderID: record.BuilderID, BuildType: record.BuildType,
		ProviderID: record.ProviderID, KeyID: record.KeyID, PolicyID: record.PolicyID,
		VerifiedAt: record.VerifiedAt.Format(time.RFC3339Nano), ExpiresAt: record.ExpiresAt.Format(time.RFC3339Nano),
	}
	return nil
}

func (s *Store) enrichSecurityAdmission(entry *Entry) error {
	reader, ok := s.repository.(VerifiedSecurityAdmissionReader)
	if !ok {
		return nil
	}
	raw, err := reader.ReadSecurityAdmission(entry.Ref)
	if err != nil {
		return err
	}
	return populateSecurityAdmission(entry, raw)
}

func populateSecurityAdmission(entry *Entry, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	record, admissionSHA, err := artifactassessment.InspectAdmission(raw)
	if err != nil || record.Evaluation.SubjectSHA256 != entry.SHA256 || entry.SBOM == nil || record.Evaluation.SBOMSHA256 != entry.SBOM.SHA256 {
		return errors.New("安全准入记录摘要、制品或 SBOM 绑定不一致")
	}
	entry.SecurityAdmission = &platformadminapi.ArtifactSecurityAdmissionDeclaration{
		AdmissionSHA256: admissionSHA, ProviderID: record.ProviderID, KeyID: record.KeyID, PolicyID: record.PolicyID,
		ScannerID: record.Evaluation.Scanner.ID, ScannerVersion: record.Evaluation.Scanner.Version, DatabaseRevision: record.Evaluation.Scanner.DatabaseRevision,
		Decision: record.Evaluation.Decision, EvaluatedAt: record.Evaluation.EvaluatedAt.Format(time.RFC3339Nano), ExpiresAt: record.Evaluation.ExpiresAt.Format(time.RFC3339Nano),
		Critical: record.Evaluation.Vulnerabilities.Critical, High: record.Evaluation.Vulnerabilities.High, Medium: record.Evaluation.Vulnerabilities.Medium,
		Low: record.Evaluation.Vulnerabilities.Low, UnknownVulnerability: record.Evaluation.Vulnerabilities.Unknown,
		DeniedLicense: record.Evaluation.Licenses.Denied, UnknownLicense: record.Evaluation.Licenses.Unknown,
	}
	return nil
}

func backendPlatforms(manifest pluginv1.Manifest) []string {
	if manifest.Execution == nil || manifest.Execution.Backend == nil {
		return nil
	}
	return append([]string(nil), manifest.Execution.Backend.Platforms...)
}

func runtimeRequires(manifest pluginv1.Manifest) []pluginv1.RuntimeRequirement {
	if manifest.Runtime == nil {
		return nil
	}
	return append([]pluginv1.RuntimeRequirement(nil), manifest.Runtime.Requires...)
}

func runtimeProvides(manifest pluginv1.Manifest) []pluginv1.RuntimeCapabilityPolicy {
	if manifest.Runtime == nil {
		return nil
	}
	return append([]pluginv1.RuntimeCapabilityPolicy(nil), manifest.Runtime.Provides...)
}

func eventFrom(entry Entry, occurredAt time.Time, recovered bool) Event {
	return Event{
		Type: "artifact.published",
		Ref:  entry.Ref, SHA256: entry.SHA256, Size: entry.Size, Publisher: entry.Publisher, KeyID: entry.KeyID,
		SignedAt: entry.SignedAt, OccurredAt: occurredAt.UTC(), Recovered: recovered, ImportSource: cloneReceipt(entry.ImportSource),
	}
}

func validateEvent(event Event, revision uint64) error {
	if event.SchemaVersion != schemaVersion || event.Revision != revision || (event.Type != "artifact.published" && event.Type != "artifact.imported" && event.Type != "artifact.lifecycle" && event.Type != "artifact.withdrawn") {
		return errors.New("不支持的流水账事件")
	}
	if event.Ref.PluginID == "" || event.Ref.Version == "" || event.Ref.Channel == "" {
		return errors.New("流水账事件缺少身份字段")
	}
	digest, err := hex.DecodeString(event.SHA256)
	if err != nil || len(digest) != 32 || event.OccurredAt.IsZero() {
		return errors.New("流水账事件的摘要、大小或时间无效")
	}
	if (event.Type == "artifact.published" || event.Type == "artifact.imported") && (event.Size <= 0 || event.Publisher == "" || event.KeyID == "" || event.SignedAt.IsZero()) {
		return errors.New("发布流水账事件缺少签名身份")
	}
	if event.Type == "artifact.imported" {
		if event.ImportSource == nil || event.ImportSource.Ref != event.Ref || event.ImportSource.SHA256 != event.SHA256 || event.ImportSource.Protocol != artifactrepositoryv1.ProtocolRemote {
			return errors.New("导入流水账事件缺少远端来源")
		}
		if err := artifactrepositoryv1.ValidateReceiptShape(*event.ImportSource); err != nil {
			return fmt.Errorf("导入流水账来源无效: %w", err)
		}
	} else if event.ImportSource != nil {
		return errors.New("非导入流水账不得携带远端来源")
	}
	if (event.Type == "artifact.lifecycle" || event.Type == "artifact.withdrawn") && (!validLifecycleStatus(event.PreviousStatus) || !validLifecycleStatus(event.Status) || event.PreviousStatus == event.Status || strings.TrimSpace(event.Reason) == "") {
		return errors.New("生命周期流水账事件无效")
	}
	if event.Type == "artifact.withdrawn" && (event.Ref.Channel != "workspace" || event.Status != LifecycleWithdrawn || event.Replacement != nil) {
		return errors.New("workspace 撤回流水账事件无效")
	}
	return nil
}

func sameIdentity(left, right Entry) bool {
	return left.Ref == right.Ref && left.SHA256 == right.SHA256 && left.Size == right.Size &&
		left.Publisher == right.Publisher && left.KeyID == right.KeyID && left.SignedAt.Equal(right.SignedAt) && sameProvenanceDeclaration(left.Provenance, right.Provenance) && sameSecurityAdmissionDeclaration(left.SecurityAdmission, right.SecurityAdmission)
}

func sameSecurityAdmissionDeclaration(left, right *platformadminapi.ArtifactSecurityAdmissionDeclaration) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameProvenanceDeclaration(left, right *platformadminapi.ArtifactProvenanceDeclaration) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func matches(entry Entry, query Query) bool {
	if query.PluginID != "" && entry.Ref.PluginID != query.PluginID {
		return false
	}
	if query.PluginPrefix != "" && entry.Ref.PluginID != query.PluginPrefix && !strings.HasPrefix(entry.Ref.PluginID, query.PluginPrefix+".") {
		return false
	}
	if query.Namespace != "" && entry.Namespace != query.Namespace {
		return false
	}
	if query.Publisher != "" && entry.Publisher != query.Publisher {
		return false
	}
	if query.Version != "" && entry.Ref.Version != query.Version {
		return false
	}
	if query.Channel != "" && entry.Ref.Channel != query.Channel {
		return false
	}
	if query.Lifecycle != "" && entry.LifecycleStatus != query.Lifecycle {
		return false
	}
	if query.Target != "" {
		found := false
		for _, target := range entry.Targets {
			if target == query.Target {
				found = true
				break
			}
		}
		return found
	}
	return true
}

func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func sortEntries(items []Entry) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Ref.PluginID != items[j].Ref.PluginID {
			return items[i].Ref.PluginID < items[j].Ref.PluginID
		}
		left, leftErr := semver.NewVersion(items[i].Ref.Version)
		right, rightErr := semver.NewVersion(items[j].Ref.Version)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.GreaterThan(right)
		}
		if items[i].Ref.Version != items[j].Ref.Version {
			return items[i].Ref.Version > items[j].Ref.Version
		}
		return items[i].Ref.Channel < items[j].Ref.Channel
	})
}

func refKey(ref pluginv1.ArtifactRef) string {
	return ref.PluginID + "@" + ref.Version + "/" + ref.Channel
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON 只能包含一个顶层值")
		}
		return err
	}
	return nil
}

func writeFileAtomically(filename string, data []byte, mode os.FileMode) error {
	return writeTemporaryAndCommit(filename, data, mode, true)
}

func writeNewFileAtomically(filename string, data []byte, mode os.FileMode) error {
	return writeTemporaryAndCommit(filename, data, mode, false)
}

func writeTemporaryAndCommit(filename string, data []byte, mode os.FileMode, replace bool) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(filename), ".tmp-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	written, writeErr := file.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if !replace {
		if err := os.Link(temporary, filename); err != nil {
			return err
		}
		return nil
	}
	return os.Rename(temporary, filename)
}

func ensurePrivateDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s 必须是普通目录且不能是符号链接", directory)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s 权限过宽 %o，要求 0700 或更严格", directory, info.Mode().Perm())
	}
	return nil
}
