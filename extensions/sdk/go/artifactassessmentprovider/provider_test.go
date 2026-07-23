package artifactassessmentprovider

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

type fakeEngine struct{ result EngineResult }

func (f fakeEngine) Scan(_ context.Context, workspace string) (EngineResult, error) {
	if workspace == "" {
		panic("missing workspace")
	}
	return f.result, nil
}

func TestProviderBindsPackageSBOMPolicyAndSignedDecision(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	packageBytes, sbom := assessmentPackage(t)
	packageDigest, sbomDigest := sha256.Sum256(packageBytes), sha256.Sum256(sbom)
	zero := uint64(0)
	provider, err := New(Config{
		ProviderID: "security.vastplan", KeyID: "test", TTL: 24 * time.Hour,
		Maximum: artifactassessment.MaximumFindings{High: &zero, DeniedLicense: &zero, UnknownLicense: &zero},
	}, privateKey, fakeEngine{result: EngineResult{
		Scanner:         artifactassessment.Scanner{ID: DefaultScannerID, Version: "0.72.0", DatabaseRevision: "db-2026-07-24"},
		Report:          []byte(`{"SchemaVersion":2,"Results":[]}`),
		Vulnerabilities: []VulnerabilityFinding{{ID: "CVE-1", Severity: SeverityHigh}},
		Licenses:        []LicenseFinding{{Name: "MIT", Disposition: LicenseAllowed}},
	}}, filepath.Join(t.TempDir(), "work"))
	if err != nil {
		t.Fatal(err)
	}
	provider.now = func() time.Time { return time.Date(2026, 7, 24, 4, 0, 0, 0, time.UTC) }
	evidence, err := provider.AssessWithEvidence(context.Background(), artifactassessment.ScanRequest{
		Identity: artifactassessment.ArtifactIdentity{PluginID: "cn.vastplan.test.assessment", Channel: "testing", Publisher: "vastplan", SHA256: hex.EncodeToString(packageDigest[:]), SBOMSHA256: hex.EncodeToString(sbomDigest[:])},
		Package:  packageBytes, SBOM: sbom, PolicyID: "testing-default",
	})
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Admission.Evaluation.Decision != artifactassessment.DecisionFail || evidence.Admission.Evaluation.Vulnerabilities.High != 1 {
		t.Fatalf("阈值决策未规范化: %+v", evidence.Admission.Evaluation)
	}
	verifier, err := artifactassessment.NewVerifier(artifactassessment.TrustPolicy{
		RequiredChannels: []string{"testing"}, MaxRecordTTLHours: 24,
		Keys:         []artifactassessment.ProviderKey{{ProviderID: "security.vastplan", KeyID: "test", PublicKey: encodeKey(publicKey)}},
		Requirements: []artifactassessment.Requirement{{ID: "testing-default", Channel: "testing", ProviderIDs: []string{"security.vastplan"}, ScannerIDs: []string{DefaultScannerID}, RequireReportDigests: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(evidence.Admission)
	if _, _, err := verifier.VerifyAdmission(artifactassessment.ArtifactIdentity{PluginID: "cn.vastplan.test.assessment", Channel: "testing", Publisher: "vastplan", SHA256: hex.EncodeToString(packageDigest[:]), SBOMSHA256: hex.EncodeToString(sbomDigest[:])}, raw, provider.now()); err == nil {
		t.Fatal("失败的 admission 必须被发布准入验证拒绝")
	}
}

func TestProviderRejectsSBOMNotBoundIntoPackage(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	packageBytes, sbom := assessmentPackage(t)
	packageDigest, sbomDigest := sha256.Sum256(packageBytes), sha256.Sum256([]byte("other"))
	provider, err := New(Config{ProviderID: "security.vastplan", KeyID: "test", TTL: time.Hour}, privateKey, fakeEngine{}, filepath.Join(t.TempDir(), "work"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Assess(context.Background(), artifactassessment.ScanRequest{
		Identity: artifactassessment.ArtifactIdentity{PluginID: "cn.vastplan.test.assessment", Channel: "testing", Publisher: "vastplan", SHA256: hex.EncodeToString(packageDigest[:]), SBOMSHA256: hex.EncodeToString(sbomDigest[:])},
		Package:  packageBytes, SBOM: append([]byte(nil), sbom...), PolicyID: "testing-default",
	})
	if err == nil {
		t.Fatal("请求摘要与实际 SBOM 不一致必须拒绝")
	}
}

func assessmentPackage(t *testing.T) ([]byte, []byte) {
	t.Helper()
	sbom := []byte(`{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"metadata":{"component":{"type":"application","name":"cn.vastplan.test.assessment","version":"1.0.0"}},"components":[]}`)
	digest := sha256.Sum256(sbom)
	manifest, err := json.Marshal(pluginv1.Manifest{
		ID: "cn.vastplan.test.assessment", Name: "assessment", Description: "assessment test", Version: "1.0.0", Publisher: "vastplan",
		Engines: map[string]string{"backend": "^0.1"}, Activation: []string{"onStartup"}, Entry: map[string]string{"backend": "backend/test"},
		SupplyChain: &pluginv1.SupplyChain{SBOM: &pluginv1.SupplyChainDocument{Format: "cyclonedx-json", SpecVersion: "1.5", Path: "supply-chain/sbom.cdx.json", SHA256: hex.EncodeToString(digest[:])}},
		Contributes: map[string]json.RawMessage{"backend": json.RawMessage(`{"tools":[]}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"vastplan.plugin.json": manifest, "supply-chain/sbom.cdx.json": sbom, "backend/test": []byte("binary")}
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"vastplan.plugin.json", "supply-chain/sbom.cdx.json", "backend/test"} {
		body := files[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes(), sbom
}

func encodeKey(key ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(key)
}
