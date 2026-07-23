package pluginservice

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

func TestSignedRepositoryRequiresAndReverifiesSecurityAdmission(t *testing.T) {
	packageBytes, artifact, sbomSHA := testArtifactWithSBOM(t)
	publisherPublic, publisherPrivate, _ := ed25519.GenerateKey(nil)
	providerPublic, providerPrivate, _ := ed25519.GenerateKey(nil)
	now := time.Now().UTC()
	zero := uint64(0)
	document := TrustDocumentForPublicKeys(TrustKey{Publisher: "example", KeyID: "publisher", PublicKey: base64.StdEncoding.EncodeToString(publisherPublic)})
	document.Assessment = &artifactassessment.TrustPolicy{
		RequiredChannels: []string{"stable"}, MaxRecordTTLHours: 48,
		Keys:         []artifactassessment.ProviderKey{{ProviderID: "assessment.static", KeyID: "scanner", PublicKey: base64.StdEncoding.EncodeToString(providerPublic)}},
		Requirements: []artifactassessment.Requirement{{ID: "stable", Channel: "stable", Publisher: "example", PluginPrefix: "com.example.", ProviderIDs: []string{"assessment.static"}, ScannerIDs: []string{"scanner.test"}, Maximum: artifactassessment.MaximumFindings{Critical: &zero, High: &zero, DeniedLicense: &zero, UnknownLicense: &zero}, RequireReportDigests: true}},
	}
	trust, err := NewTrustStore(document)
	if err != nil {
		t.Fatal(err)
	}
	evaluation := artifactassessment.Evaluation{
		SubjectSHA256: artifact.SHA256, SBOMSHA256: sbomSHA,
		Scanner:         artifactassessment.Scanner{ID: "scanner.test", Version: "1.0.0", DatabaseRevision: "db-2026-07-24"},
		Vulnerabilities: artifactassessment.VulnerabilitySummary{ReportSHA256: digestTestBytes([]byte("vulnerability report"))},
		Licenses:        artifactassessment.LicenseSummary{Allowed: 1, ReportSHA256: digestTestBytes([]byte("license report"))},
		Decision:        artifactassessment.DecisionPass, EvaluatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}
	record, err := artifactassessment.SignAdmission(artifactassessment.AdmissionRecord{Evaluation: evaluation, ProviderID: "assessment.static", KeyID: "scanner", PolicyID: "stable"}, providerPrivate)
	if err != nil {
		t.Fatal(err)
	}
	admissionRaw, _ := json.Marshal(record)
	local, _ := NewRepository(filepath.Join(t.TempDir(), "repository"))
	repository := &SignedRepository{Local: local, Trust: trust}
	attestation, _ := SignArtifact(artifact, "example", "publisher", publisherPrivate, now)
	if _, err := repository.Publish(attestation, packageBytes); err == nil {
		t.Fatal("stable 缺少安全准入记录必须拒绝")
	}
	if _, err := repository.PublishWithSupplyChain(attestation, packageBytes, nil, nil, admissionRaw); err != nil {
		t.Fatal(err)
	}
	ref := Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	_, _, _, _, _, stored, err := repository.ReadWithSupplyChain(ref)
	if err != nil || !bytes.Equal(stored, admissionRaw) {
		t.Fatalf("安全准入记录未按原始字节保存和复验: %v", err)
	}
	mutated := append([]byte(nil), admissionRaw...)
	mutated[len(mutated)-2] ^= 1
	if _, err := repository.PublishWithSupplyChain(attestation, packageBytes, nil, nil, mutated); err == nil {
		t.Fatal("同 ref 替换安全准入记录必须拒绝")
	}
	dir, _ := local.artifactDir(ref)
	if err := os.Remove(filepath.Join(dir, "security-admission.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ReadMetadataWithAttestation(ref); err == nil {
		t.Fatal("安全准入记录缺失时必须 fail-closed")
	}
}

func testArtifactWithSBOM(t *testing.T) ([]byte, Artifact, string) {
	t.Helper()
	directory := writeTestPlugin(t)
	sbom := []byte(`{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"metadata":{"component":{"type":"application","name":"com.example.package-test","version":"1.2.3"}},"components":[{"type":"library","name":"example","version":"2.0.0"}]}`)
	sbomSHA := digestTestBytes(sbom)
	if err := os.MkdirAll(filepath.Join(directory, "supply-chain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "supply-chain", "sbom.cdx.json"), sbom, 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(directory, manifestName)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"activation"`), []byte(`"supplyChain":{"sbom":{"format":"cyclonedx-json","specVersion":"1.5","path":"supply-chain/sbom.cdx.json","sha256":"`+sbomSHA+`"}},"activation"`), 1)
	if err := os.WriteFile(manifestPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	packageBytes, _, err := PackageDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Describe("stable", packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	return packageBytes, artifact, sbomSHA
}

func digestTestBytes(raw []byte) string {
	value := sha256.Sum256(raw)
	return hex.EncodeToString(value[:])
}
