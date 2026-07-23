package catalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactprovenance"
)

type bundleReportSource struct {
	*pluginservice.SignedRepository
	reports map[string][]byte
}

func (s bundleReportSource) ReadAssessmentReport(digest string) ([]byte, error) {
	return append([]byte(nil), s.reports[digest]...), nil
}

type bundleReportDestination struct {
	pluginservice.HTTPRepositoryAdapter
	signed  *pluginservice.SignedRepository
	reports map[string][]byte
}

func (d *bundleReportDestination) PutAssessmentReport(digest string, raw []byte) error {
	d.reports[digest] = append([]byte(nil), raw...)
	return nil
}

func (d *bundleReportDestination) AppendSecurityStatus(ref pluginservice.Ref, raw []byte, now time.Time) (*artifactassessment.StatusRecord, string, error) {
	return d.signed.AppendSecurityStatus(ref, raw, now)
}

func TestOfflineBundleIsDeterministicAndContainsLockedProofs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trustDocument := pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{
		Publisher: "example", KeyID: "testing", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	})
	trust, err := pluginservice.NewTrustStore(trustDocument)
	if err != nil {
		t.Fatal(err)
	}
	local, err := pluginservice.NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	repository := &pluginservice.SignedRepository{Local: local, Trust: trust}
	artifact, _ := publishTestArtifact(t, repository, privateKey, "1.0.0-dev.20260721.1.abcdef0")
	store, err := Open(root, repository)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := store.Resolve(pluginv1.ArtifactResolveRequest{
		Roots:  []pluginv1.ArtifactRequirement{{PluginID: artifact.PluginID, Constraint: "=" + artifact.Version}},
		Target: "backend", KernelVersion: "0.1.0", AllowedChannels: []string{"testing"},
		AllowedPublishers: []string{"example"}, AllowedPluginPrefixes: []string{"com.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	trustRaw, err := json.Marshal(trustDocument)
	if err != nil {
		t.Fatal(err)
	}
	bundleDirectory := filepath.Join(t.TempDir(), "bundles")
	first, err := CreateOfflineBundle(lock, trustRaw, repository, bundleDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(first.Path)
	second, err := CreateOfflineBundle(lock, trustRaw, repository, bundleDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(second.Path)
	if first.SHA256 != second.SHA256 || first.Size != second.Size {
		t.Fatalf("same lock and trust snapshot must yield identical bundle: first=%+v second=%+v", first, second)
	}
	contents := readOfflineBundle(t, first.Path)
	for _, name := range []string{
		"vastplan.lock.json", "trust.json", "bundle.manifest.json",
		"artifacts/" + artifact.SHA256 + "/package.tar.gz",
		"artifacts/" + artifact.SHA256 + "/attestation.json",
	} {
		if _, ok := contents[name]; !ok {
			t.Fatalf("offline bundle missing %s: %v", name, mapKeys(contents))
		}
	}
	var embeddedLock pluginv1.ArtifactLock
	if err := json.Unmarshal(contents["vastplan.lock.json"], &embeddedLock); err != nil || embeddedLock.Digest != lock.Digest {
		t.Fatalf("embedded lock changed: lock=%#v err=%v", embeddedLock, err)
	}

	destinationLocal, err := pluginservice.NewRepository(filepath.Join(t.TempDir(), "imported"))
	if err != nil {
		t.Fatal(err)
	}
	destination := &pluginservice.SignedRepository{Local: destinationLocal, Trust: trust}
	importedLock, err := ImportOfflineBundle(first.Path, pluginservice.HTTPRepositoryAdapter{Repository: destination})
	if err != nil {
		t.Fatal(err)
	}
	if importedLock.Digest != lock.Digest {
		t.Fatalf("import changed lock: got=%s want=%s", importedLock.Digest, lock.Digest)
	}
	if imported, _, _, err := destination.ReadWithAttestation(pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}); err != nil || imported.SHA256 != artifact.SHA256 {
		t.Fatalf("imported artifact did not pass destination trust boundary: artifact=%#v err=%v", imported, err)
	}

	httpDestinationLocal, err := pluginservice.NewRepository(filepath.Join(t.TempDir(), "http-imported"))
	if err != nil {
		t.Fatal(err)
	}
	httpDestination := &pluginservice.SignedRepository{Local: httpDestinationLocal, Trust: trust}
	handler := &HTTPHandler{
		Store: store, ReadToken: "reader", BundleToken: "bundle", ImportToken: "publisher",
		BundleSource: repository, BundleDestination: pluginservice.HTTPRepositoryAdapter{Repository: httpDestination},
		TrustSnapshot: trustRaw, BundleDirectory: bundleDirectory, RequireTLS: true,
	}
	lockRequest, err := json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if status := bundleHTTP(t, handler, "/v1/catalog/bundles", "reader", lockRequest).Code; status != http.StatusUnauthorized {
		t.Fatalf("read token must not authorize bundle export: %d", status)
	}
	exported := bundleHTTP(t, handler, "/v1/catalog/bundles", "bundle", lockRequest)
	if exported.Code != http.StatusOK || exported.Header().Get("X-VastPlan-Bundle-SHA256") == "" {
		t.Fatalf("bundle export failed: status=%d body=%s", exported.Code, exported.Body.String())
	}
	if status := bundleHTTP(t, handler, "/v1/catalog/bundles/import", "bundle", exported.Body.Bytes()).Code; status != http.StatusUnauthorized {
		t.Fatalf("bundle export token must not authorize import: %d", status)
	}
	imported := bundleHTTP(t, handler, "/v1/catalog/bundles/import", "publisher", exported.Body.Bytes())
	if imported.Code != http.StatusOK {
		t.Fatalf("bundle HTTP import failed: status=%d body=%s", imported.Code, imported.Body.String())
	}
}

func TestOfflineBundlePreservesAndReverifiesProvenance(t *testing.T) {
	publisherPublic, publisherPrivate, _ := ed25519.GenerateKey(nil)
	providerPublic, providerPrivate, _ := ed25519.GenerateKey(nil)
	packageBytes, artifact := bundleTestArtifact(t)
	now := time.Now().UTC()
	provenanceRaw := bundleTestDSSE(t, artifact.SHA256)
	summary, provenanceSHA, err := artifactprovenance.InspectDSSE(provenanceRaw, artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	record, err := artifactprovenance.SignRecord(artifactprovenance.VerificationRecord{
		SubjectSHA256: artifact.SHA256, ProvenanceSHA256: provenanceSHA, StatementSummary: summary,
		ProviderID: "provider.static", KeyID: "provider-key", PolicyID: "testing", VerifiedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}, providerPrivate)
	if err != nil {
		t.Fatal(err)
	}
	verificationRaw, _ := json.Marshal(record)
	trustDocument := pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{Publisher: "example", KeyID: "testing", PublicKey: base64.StdEncoding.EncodeToString(publisherPublic)})
	trustDocument.Provenance = &artifactprovenance.TrustPolicy{
		RequiredChannels: []string{"testing"}, MaxRecordTTLHours: 48,
		Keys:         []artifactprovenance.VerifierKey{{ProviderID: record.ProviderID, KeyID: record.KeyID, PublicKey: base64.StdEncoding.EncodeToString(providerPublic)}},
		Requirements: []artifactprovenance.Requirement{{ID: "testing", Channel: "testing", Publisher: "example", PluginPrefix: "com.example.", ProviderIDs: []string{record.ProviderID}, BuilderIDs: []string{summary.BuilderID}, BuildTypes: []string{summary.BuildType}, SourceURIPrefixes: []string{"git+https://example.com/"}, RequireSourceDigest: true}},
	}
	trust, err := pluginservice.NewTrustStore(trustDocument)
	if err != nil {
		t.Fatal(err)
	}
	sourceRoot := filepath.Join(t.TempDir(), "source")
	sourceLocal, _ := pluginservice.NewRepository(sourceRoot)
	source := &pluginservice.SignedRepository{Local: sourceLocal, Trust: trust}
	attestation, _ := pluginservice.SignArtifact(artifact, "example", "testing", publisherPrivate, now)
	if _, err := source.PublishWithProvenance(attestation, packageBytes, provenanceRaw, verificationRaw); err != nil {
		t.Fatal(err)
	}
	store, err := Open(sourceRoot, source)
	if err != nil {
		t.Fatal(err)
	}
	page := store.Query(Query{PluginID: artifact.PluginID, Page: 1, PageSize: 10})
	if len(page.Items) != 1 || page.Items[0].Provenance == nil || page.Items[0].Provenance.ProviderID != "provider.static" {
		t.Fatalf("Catalog 未索引已验证来源证明摘要: %+v", page.Items)
	}
	evidence, err := store.Evidence(pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel})
	if err != nil || evidence.Provenance == nil || evidence.Provenance.Verification != "verified" {
		t.Fatalf("供应链证据未复验来源证明: evidence=%+v err=%v", evidence, err)
	}
	lock, err := store.Resolve(pluginv1.ArtifactResolveRequest{Roots: []pluginv1.ArtifactRequirement{{PluginID: artifact.PluginID, Constraint: "=" + artifact.Version}}, Target: "backend", KernelVersion: "0.1.0", AllowedChannels: []string{"testing"}, AllowedPublishers: []string{"example"}, AllowedPluginPrefixes: []string{"com.example"}})
	if err != nil {
		t.Fatal(err)
	}
	trustRaw, _ := json.Marshal(trustDocument)
	bundle, err := CreateOfflineBundle(lock, trustRaw, source, filepath.Join(t.TempDir(), "bundles"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(bundle.Path)
	contents := readOfflineBundle(t, bundle.Path)
	base := "artifacts/" + artifact.SHA256
	for _, name := range []string{base + "/provenance.dsse.json", base + "/provenance-verification.json"} {
		if _, ok := contents[name]; !ok {
			t.Fatalf("离线 Bundle 缺少来源证明 %s", name)
		}
	}
	destinationLocal, _ := pluginservice.NewRepository(filepath.Join(t.TempDir(), "destination"))
	destination := &pluginservice.SignedRepository{Local: destinationLocal, Trust: trust}
	if _, err := ImportOfflineBundle(bundle.Path, pluginservice.HTTPRepositoryAdapter{Repository: destination}); err != nil {
		t.Fatal(err)
	}
	_, _, _, importedProvenance, importedVerification, err := destination.ReadWithProvenance(pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel})
	if err != nil || string(importedProvenance) != string(provenanceRaw) || string(importedVerification) != string(verificationRaw) {
		t.Fatalf("离线导入没有原样保留来源证明: %v", err)
	}
}

func TestOfflineBundleCarriesAssessmentReportsAndStatusChain(t *testing.T) {
	packageBytes, artifact, sbomDigest := bundleAssessmentArtifact(t)
	publisherPublic, publisherPrivate, _ := ed25519.GenerateKey(nil)
	providerPublic, providerPrivate, _ := ed25519.GenerateKey(nil)
	report := []byte(`{"SchemaVersion":2,"Results":[]}`)
	reportDigest := digestBundleBytes(report)
	now := time.Now().UTC().Add(-time.Hour)
	zero := uint64(0)
	trustDocument := pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{Publisher: "example", KeyID: "publisher", PublicKey: base64.StdEncoding.EncodeToString(publisherPublic)})
	trustDocument.Assessment = &artifactassessment.TrustPolicy{
		RequiredChannels: []string{"stable"}, MaxRecordTTLHours: 48,
		Keys:         []artifactassessment.ProviderKey{{ProviderID: "assessment.bundle", KeyID: "scanner", PublicKey: base64.StdEncoding.EncodeToString(providerPublic)}},
		Requirements: []artifactassessment.Requirement{{ID: "stable", Channel: "stable", Publisher: "example", PluginPrefix: "com.example.", ProviderIDs: []string{"assessment.bundle"}, ScannerIDs: []string{"scanner.test"}, Maximum: artifactassessment.MaximumFindings{Critical: &zero, High: &zero, DeniedLicense: &zero, UnknownLicense: &zero}, RequireReportDigests: true}},
	}
	trust, err := pluginservice.NewTrustStore(trustDocument)
	if err != nil {
		t.Fatal(err)
	}
	evaluation := artifactassessment.Evaluation{
		SubjectSHA256: artifact.SHA256, SBOMSHA256: sbomDigest,
		Scanner:         artifactassessment.Scanner{ID: "scanner.test", Version: "1", DatabaseRevision: "db-bundle"},
		Vulnerabilities: artifactassessment.VulnerabilitySummary{ReportSHA256: reportDigest}, Licenses: artifactassessment.LicenseSummary{Allowed: 1, ReportSHA256: reportDigest},
		Decision: artifactassessment.DecisionPass, EvaluatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}
	admission, err := artifactassessment.SignAdmission(artifactassessment.AdmissionRecord{Evaluation: evaluation, ProviderID: "assessment.bundle", KeyID: "scanner", PolicyID: "stable"}, providerPrivate)
	if err != nil {
		t.Fatal(err)
	}
	admissionRaw, _ := json.Marshal(admission)
	_, admissionDigest, err := artifactassessment.InspectAdmission(admissionRaw)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "source")
	local, _ := pluginservice.NewRepository(root)
	signed := &pluginservice.SignedRepository{Local: local, Trust: trust}
	attestation, _ := pluginservice.SignArtifact(artifact, "example", "publisher", publisherPrivate, now)
	if _, err := signed.PublishWithSupplyChain(attestation, packageBytes, nil, nil, admissionRaw); err != nil {
		t.Fatal(err)
	}
	statusReport := []byte(`{"SchemaVersion":2,"Results":[{"Target":"updated"}]}`)
	statusReportDigest := digestBundleBytes(statusReport)
	statusEvaluation := evaluation
	statusEvaluation.Scanner.DatabaseRevision = "db-bundle-updated"
	statusEvaluation.Vulnerabilities.ReportSHA256 = statusReportDigest
	statusEvaluation.Licenses.ReportSHA256 = statusReportDigest
	statusEvaluation.EvaluatedAt = now.Add(15 * time.Minute)
	statusEvaluation.ExpiresAt = now.Add(25 * time.Hour)
	status, err := artifactassessment.SignStatus(artifactassessment.StatusRecord{
		AdmissionSHA256: admissionDigest, Sequence: 1, PreviousSHA256: admissionDigest,
		Evaluation: statusEvaluation, ProviderID: "assessment.bundle", KeyID: "scanner", PolicyID: "stable",
	}, providerPrivate)
	if err != nil {
		t.Fatal(err)
	}
	statusRaw, _ := json.Marshal(status)
	ref := pluginservice.Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	if _, _, err := signed.AppendSecurityStatus(ref, statusRaw, statusEvaluation.EvaluatedAt); err != nil {
		t.Fatal(err)
	}
	store, err := Open(root, signed)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := store.Resolve(pluginv1.ArtifactResolveRequest{Roots: []pluginv1.ArtifactRequirement{{PluginID: artifact.PluginID, Constraint: "=" + artifact.Version}}, Target: "backend", KernelVersion: "0.1.0", AllowedChannels: []string{"stable"}, AllowedPublishers: []string{"example"}, AllowedPluginPrefixes: []string{"com.example"}})
	if err != nil {
		t.Fatal(err)
	}
	trustRaw, _ := json.Marshal(trustDocument)
	source := bundleReportSource{SignedRepository: signed, reports: map[string][]byte{reportDigest: report, statusReportDigest: statusReport}}
	bundle, err := CreateOfflineBundle(lock, trustRaw, source, filepath.Join(t.TempDir(), "bundles"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(bundle.Path)
	contents := readOfflineBundle(t, bundle.Path)
	if raw := contents["reports/"+reportDigest+".json"]; !bytes.Equal(raw, report) {
		t.Fatal("离线 Bundle 未携带准入记录引用的原始报告")
	}
	if raw := contents["reports/"+statusReportDigest+".json"]; !bytes.Equal(raw, statusReport) {
		t.Fatal("离线 Bundle 未携带复扫状态引用的原始报告")
	}
	if raw := contents["artifacts/"+artifact.SHA256+"/security-status-chain.json"]; len(raw) == 0 {
		t.Fatal("离线 Bundle 未携带安全复扫状态链")
	}
	destinationLocal, _ := pluginservice.NewRepository(filepath.Join(t.TempDir(), "destination"))
	destinationSigned := &pluginservice.SignedRepository{Local: destinationLocal, Trust: trust}
	destination := &bundleReportDestination{HTTPRepositoryAdapter: pluginservice.HTTPRepositoryAdapter{Repository: destinationSigned}, signed: destinationSigned, reports: map[string][]byte{}}
	if _, err := ImportOfflineBundle(bundle.Path, destination); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(destination.reports[reportDigest], report) {
		t.Fatal("离线导入未在发布 AdmissionRecord 前恢复原始报告")
	}
	if !bytes.Equal(destination.reports[statusReportDigest], statusReport) {
		t.Fatal("离线导入未在追加复扫状态前恢复原始报告")
	}
	chainRaw, err := destinationSigned.ReadSecurityStatusChain(ref)
	if err != nil {
		t.Fatal(err)
	}
	records, err := artifactassessment.InspectStatusChain(chainRaw)
	if err != nil || len(records) != 1 || !bytes.Equal(records[0], statusRaw) {
		t.Fatalf("离线导入没有原样恢复复扫状态链: records=%d err=%v", len(records), err)
	}
}

func bundleTestArtifact(t *testing.T) ([]byte, pluginservice.Artifact) {
	t.Helper()
	directory := t.TempDir()
	manifest := []byte(`{"id":"com.example.bundle-provenance","name":"Bundle provenance","description":"test","version":"1.0.0","publisher":"example","engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`)
	if err := os.WriteFile(filepath.Join(directory, "vastplan.plugin.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "backend"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "backend", "main"), []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	packageBytes, _, err := pluginservice.PackageDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe("testing", packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	return packageBytes, artifact
}

func bundleAssessmentArtifact(t *testing.T) ([]byte, pluginservice.Artifact, string) {
	t.Helper()
	directory := t.TempDir()
	sbom := []byte(`{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"metadata":{"component":{"type":"application","name":"com.example.bundle-assessment","version":"1.0.0"}},"components":[]}`)
	sbomDigest := digestBundleBytes(sbom)
	manifest := []byte(`{"id":"com.example.bundle-assessment","name":"Bundle assessment","description":"test","version":"1.0.0","publisher":"example","engines":{"backend":"^0.1"},"supplyChain":{"sbom":{"format":"cyclonedx-json","specVersion":"1.5","path":"supply-chain/sbom.cdx.json","sha256":"` + sbomDigest + `"}},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`)
	if err := os.MkdirAll(filepath.Join(directory, "backend"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "supply-chain"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, raw := range map[string][]byte{"vastplan.plugin.json": manifest, "backend/main": []byte("binary"), "supply-chain/sbom.cdx.json": sbom} {
		if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(path)), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	packageBytes, _, err := pluginservice.PackageDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe("stable", packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	return packageBytes, artifact, sbomDigest
}

func digestBundleBytes(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func bundleTestDSSE(t *testing.T, subject string) []byte {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"_type": artifactprovenance.InTotoStatementType, "subject": []any{map[string]any{"name": "plugin.tar.gz", "digest": map[string]string{"sha256": subject}}}, "predicateType": artifactprovenance.SLSAProvenanceType,
		"predicate": map[string]any{"buildDefinition": map[string]any{"buildType": "plugin-build", "resolvedDependencies": []any{map[string]any{"uri": "git+https://example.com/repository", "digest": map[string]string{"gitCommit": "abc"}}}}, "runDetails": map[string]any{"builder": map[string]string{"id": "builder"}}},
	})
	raw, _ := json.Marshal(map[string]any{"payloadType": artifactprovenance.DSSEPayloadType, "payload": base64.StdEncoding.EncodeToString(payload), "signatures": []any{map[string]string{"keyid": "builder", "sig": "verified-externally"}}})
	return raw
}

func bundleHTTP(t *testing.T, handler http.Handler, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://repository.test"+path, bytes.NewReader(body))
	request.TLS = &tls.ConnectionState{HandshakeComplete: true}
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func readOfflineBundle(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	contents := map[string][]byte{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		contents[header.Name] = raw
	}
	return contents
}

func mapKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
