package pluginservice

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactprovenance"
)

func TestSignedRepositoryRequiresAndReverifiesProvenanceSidecars(t *testing.T) {
	packageBytes, artifact := testArtifact(t)
	publisherPublic, publisherPrivate, _ := ed25519.GenerateKey(nil)
	now := time.Now().UTC()
	fixture := provenanceTrustForArtifact(t, artifact, publisherPublic, now)
	root := filepath.Join(t.TempDir(), "repository")
	local, _ := NewRepository(root)
	repository := &SignedRepository{Local: local, Trust: fixture.trust}
	attestation, _ := SignArtifact(artifact, "example", "publisher-key", publisherPrivate, now)
	if _, err := repository.Publish(attestation, packageBytes); err == nil {
		t.Fatal("stable 缺少来源证明必须拒绝")
	}
	if _, err := repository.PublishWithProvenance(attestation, packageBytes, fixture.provenance, fixture.verification); err != nil {
		t.Fatal(err)
	}
	ref := Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	_, _, _, storedProvenance, storedVerification, err := repository.ReadWithProvenance(ref)
	if err != nil || string(storedProvenance) != string(fixture.provenance) || string(storedVerification) != string(fixture.verification) {
		t.Fatalf("来源证明 sidecar 未按原始字节复验: err=%v", err)
	}
	metadata, metadataProof, metadataProvenance, metadataVerification, err := repository.ReadMetadataWithProvenance(ref)
	if err != nil || metadata.SHA256 != artifact.SHA256 || len(metadataProof) == 0 || string(metadataProvenance) != string(fixture.provenance) || string(metadataVerification) != string(fixture.verification) {
		t.Fatalf("Catalog 单次元数据读取未返回完整可信 sidecar: metadata=%#v err=%v", metadata, err)
	}
	mutated := append([]byte(nil), fixture.verification...)
	mutated[len(mutated)-1] ^= 1
	if _, err := repository.PublishWithProvenance(attestation, packageBytes, fixture.provenance, mutated); err == nil {
		t.Fatal("同 ref 替换 Verification Record 必须拒绝")
	}
	dir, _ := local.artifactDir(ref)
	if err := os.Remove(filepath.Join(dir, "provenance-verification.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repository.ReadMetadataWithAttestation(ref); err == nil {
		t.Fatal("恢复路径缺少一半 sidecar 必须 fail-closed")
	}
}

type provenanceTrustFixture struct {
	trust                    *TrustStore
	provenance, verification []byte
}

func provenanceTrustForArtifact(t *testing.T, artifact Artifact, publisherPublic ed25519.PublicKey, now time.Time) provenanceTrustFixture {
	t.Helper()
	providerPublic, providerPrivate, _ := ed25519.GenerateKey(nil)
	provenanceRaw := testDSSEForArtifact(t, artifact.SHA256)
	summary, provenanceSHA, err := artifactprovenance.InspectDSSE(provenanceRaw, artifact.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	record, err := artifactprovenance.SignRecord(artifactprovenance.VerificationRecord{
		SubjectSHA256: artifact.SHA256, ProvenanceSHA256: provenanceSHA, StatementSummary: summary,
		ProviderID: "provider.static", KeyID: "provider-key", PolicyID: "stable", VerifiedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}, providerPrivate)
	if err != nil {
		t.Fatal(err)
	}
	verificationRaw, _ := json.Marshal(record)
	trustDocument := TrustDocumentForPublicKeys(TrustKey{Publisher: "example", KeyID: "publisher-key", PublicKey: base64.StdEncoding.EncodeToString(publisherPublic)})
	trustDocument.Provenance = &artifactprovenance.TrustPolicy{
		RequiredChannels: []string{"stable"}, MaxRecordTTLHours: 48,
		Keys:         []artifactprovenance.VerifierKey{{ProviderID: record.ProviderID, KeyID: record.KeyID, PublicKey: base64.StdEncoding.EncodeToString(providerPublic)}},
		Requirements: []artifactprovenance.Requirement{{ID: "stable", Channel: "stable", Publisher: "example", PluginPrefix: "com.example.", ProviderIDs: []string{record.ProviderID}, BuilderIDs: []string{summary.BuilderID}, BuildTypes: []string{summary.BuildType}, SourceURIPrefixes: []string{"git+https://example.com/"}, RequireSourceDigest: true}},
	}
	trust, err := NewTrustStore(trustDocument)
	if err != nil {
		t.Fatal(err)
	}
	return provenanceTrustFixture{trust: trust, provenance: provenanceRaw, verification: verificationRaw}
}

func testDSSEForArtifact(t *testing.T, sha string) []byte {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"_type":         artifactprovenance.InTotoStatementType,
		"subject":       []any{map[string]any{"name": "plugin.tar.gz", "digest": map[string]string{"sha256": sha}}},
		"predicateType": artifactprovenance.SLSAProvenanceType,
		"predicate": map[string]any{
			"buildDefinition": map[string]any{"buildType": "plugin-build", "resolvedDependencies": []any{map[string]any{"uri": "git+https://example.com/repository", "digest": map[string]string{"gitCommit": "abc"}}}},
			"runDetails":      map[string]any{"builder": map[string]string{"id": "builder"}},
		},
	})
	raw, _ := json.Marshal(map[string]any{"payloadType": artifactprovenance.DSSEPayloadType, "payload": base64.StdEncoding.EncodeToString(payload), "signatures": []any{map[string]string{"keyid": "builder", "sig": "external-provider-verified"}}})
	return raw
}
