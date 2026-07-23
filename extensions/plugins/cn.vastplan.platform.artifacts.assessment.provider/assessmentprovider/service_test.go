package assessmentprovider

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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
	provider "cdsoft.com.cn/VastPlan/extensions/sdk/go/artifactassessmentprovider"
)

type fixedDownloader struct{ raw []byte }

func (d fixedDownloader) Download(context.Context, artifactassessment.ScanLease) ([]byte, error) {
	return append([]byte(nil), d.raw...), nil
}

type fixedEngine struct{ result provider.EngineResult }

func (e fixedEngine) Scan(context.Context, string) (provider.EngineResult, error) {
	return e.result, nil
}

type serviceHost struct {
	now                            time.Time
	ref                            commonv1.ManagedCredentialRef
	secret                         []byte
	audience                       string
	lease                          artifactassessment.ScanLease
	repositoryCalls, materialCalls int
}

func (h *serviceHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	switch target.GetCapability() {
	case "platform.artifacts.repository":
		h.repositoryCalls++
		raw, _ := json.Marshal(h.lease)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	case credentiallease.RuntimeKernelService:
		h.materialCalls++
		var request credentiallease.Request
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, nil, err
		}
		envelope, err := credentiallease.Seal(request, credentiallease.Claims{TenantID: "tenant-a", Audience: h.audience, Ref: h.ref}, h.secret, h.now, 15*time.Second)
		if err != nil {
			return nil, nil, err
		}
		raw, _ := json.Marshal(envelope)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	default:
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR}, nil, nil
	}
}

func TestServiceUsesScanLeaseAndMaterialLeaseWithoutReturningSecrets(t *testing.T) {
	now := time.Date(2026, 7, 24, 6, 0, 0, 0, time.UTC)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	packageBytes, sbom := servicePackage(t)
	packageDigest, sbomDigest := sha256.Sum256(packageBytes), sha256.Sum256(sbom)
	ref := pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.assessment-test", Version: "1.0.0", Channel: "testing"}
	credentialRef := commonv1.ManagedCredentialRef{Handle: "credential://managed/assessment-key", Scope: "tenant", Owner: PluginID, Purpose: SigningPurpose, Version: 1}
	identity := runtimeidentity.Identity{PluginID: PluginID, Publisher: "vastplan", Version: PluginVersion, ArtifactSHA256: strings.Repeat("c", 64), NodeID: "node-a", RuntimeScope: "assessment", InstanceID: "provider-a"}
	audience, err := identity.Audience()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(protocol.RuntimeAudienceEnvKey, audience)
	root := t.TempDir()
	config := Config{
		ProviderID: "security.vastplan", KeyID: "release", SigningKeyRef: credentialRef,
		TrivyBinary: "/trivy", TrivyCacheDirectory: "/cache", ScannerVersion: "1", DatabaseRevision: strings.Repeat("d", 64),
		WorkRoot: filepath.Join(root, "work"), ReportRoot: filepath.Join(root, "reports"), TTLHours: 24, TimeoutSeconds: 60,
		AllowedLicenses: []string{"MIT"}, Maximum: artifactassessment.MaximumFindings{},
	}
	service, err := New(config, fixedEngine{result: provider.EngineResult{
		Scanner: artifactassessment.Scanner{ID: provider.DefaultScannerID, Version: "1", DatabaseRevision: strings.Repeat("d", 64)},
		Report:  []byte(`{"SchemaVersion":2,"Results":[{"Packages":[{}]}]}`), Licenses: []provider.LicenseFinding{{Name: "MIT", Disposition: provider.LicenseAllowed}},
	}}, fixedDownloader{raw: packageBytes})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	host := &serviceHost{now: now, ref: credentialRef, secret: append([]byte(nil), privateKey...), audience: audience, lease: artifactassessment.ScanLease{
		SchemaVersion: artifactassessment.SchemaVersion, Ref: ref, SubjectSHA256: hex.EncodeToString(packageDigest[:]), SBOMSHA256: hex.EncodeToString(sbomDigest[:]),
		Audience: PluginID, URL: "https://repo.example/v1/artifacts/cn.vastplan.product.assessment-test/1.0.0/testing/package?vp_ticket=" + strings.Repeat("e", 43), ExpiresAt: now.Add(artifactassessment.ScanLeaseTTL),
	}}
	request, _ := json.Marshal(artifactassessment.ProviderAssessmentRequest{ScanLeaseRequest: artifactassessment.ScanLeaseRequest{Ref: ref, SubjectSHA256: hex.EncodeToString(packageDigest[:]), SBOMSHA256: hex.EncodeToString(sbomDigest[:])}, PolicyID: "testing-default"})
	raw, err := service.AssessAdmission(context.Background(), host, &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: artifactassessment.AssessmentControllerPluginID}}, request)
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := artifactassessment.InspectAdmission(raw)
	if err != nil || record.ProviderID != "security.vastplan" || host.repositoryCalls != 1 || host.materialCalls != 1 {
		t.Fatalf("运行态评估链路无效: record=%+v err=%v", record, err)
	}
	policy, err := artifactassessment.NewVerifier(artifactassessment.TrustPolicy{RequiredChannels: []string{"testing"}, MaxRecordTTLHours: 24,
		Keys:         []artifactassessment.ProviderKey{{ProviderID: "security.vastplan", KeyID: "release", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}},
		Requirements: []artifactassessment.Requirement{{ID: "testing-default", Channel: "testing", ProviderIDs: []string{"security.vastplan"}, ScannerIDs: []string{provider.DefaultScannerID}, RequireReportDigests: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := policy.VerifyAdmission(artifactassessment.ArtifactIdentity{PluginID: ref.PluginID, Channel: ref.Channel, Publisher: "vastplan", SHA256: hex.EncodeToString(packageDigest[:]), SBOMSHA256: hex.EncodeToString(sbomDigest[:])}, raw, now); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, privateKey) {
		t.Fatal("Provider 响应不得包含私钥 material")
	}
	if _, err := os.Stat(filepath.Join(config.ReportRoot, record.Evaluation.Vulnerabilities.ReportSHA256+".json")); err != nil {
		t.Fatal("原始报告未按摘要归档")
	}
}

func servicePackage(t *testing.T) ([]byte, []byte) {
	t.Helper()
	sbom := []byte(`{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"metadata":{"component":{"name":"cn.vastplan.product.assessment-test","version":"1.0.0"}},"components":[]}`)
	digest := sha256.Sum256(sbom)
	manifest, _ := json.Marshal(pluginv1.Manifest{ID: "cn.vastplan.product.assessment-test", Name: "test", Description: "test", Version: "1.0.0", Publisher: "vastplan", Engines: map[string]string{"backend": "^0.1"}, Activation: []string{"onStartup"}, Entry: map[string]string{"backend": "backend/test"}, SupplyChain: &pluginv1.SupplyChain{SBOM: &pluginv1.SupplyChainDocument{Format: "cyclonedx-json", SpecVersion: "1.5", Path: "supply-chain/sbom.cdx.json", SHA256: hex.EncodeToString(digest[:])}}, Contributes: map[string]json.RawMessage{"backend": json.RawMessage(`{"tools":[]}`)}})
	files := []struct {
		name string
		raw  []byte
	}{{"vastplan.plugin.json", manifest}, {"supply-chain/sbom.cdx.json", sbom}, {"backend/test", []byte("binary")}}
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	for _, file := range files {
		if err := tw.WriteHeader(&tar.Header{Name: file.name, Mode: 0o600, Size: int64(len(file.raw))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(file.raw); err != nil {
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
