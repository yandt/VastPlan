package artifactprovenance

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestVerifierBindsDSSERecordArtifactAndPolicy(t *testing.T) {
	artifactDigest := sha256.Sum256([]byte("plugin-package"))
	artifactSHA := hex.EncodeToString(artifactDigest[:])
	builderPublic, builderPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	provenance := signedTestDSSE(t, artifactSHA, "builder-key", builderPrivate)
	summary, provenanceSHA, err := VerifyDSSEEd25519(provenance, artifactSHA, map[string]ed25519.PublicKey{"builder-key": builderPublic})
	if err != nil {
		t.Fatal(err)
	}
	providerPublic, providerPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 24, 1, 0, 0, 0, time.UTC)
	record, err := SignRecord(VerificationRecord{
		SubjectSHA256: artifactSHA, ProvenanceSHA256: provenanceSHA, StatementSummary: summary,
		ProviderID: "platform.artifacts.provenance.static", KeyID: "provider-2026", PolicyID: "vastplan-stable",
		Issuer: "https://issuer.example.com", Workflow: "release-plugin.yaml", VerifiedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}, providerPrivate)
	if err != nil {
		t.Fatal(err)
	}
	recordRaw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateVerifyRequest(VerifyRequest{SubjectSHA256: artifactSHA, PolicyID: "vastplan-stable", Provenance: provenance}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateVerifyResult(VerifyResult{Record: recordRaw}, artifactSHA, "vastplan-stable"); err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(TrustPolicy{
		RequiredChannels: []string{"stable"}, MaxRecordTTLHours: 48,
		Keys: []VerifierKey{{ProviderID: record.ProviderID, KeyID: record.KeyID, PublicKey: base64.StdEncoding.EncodeToString(providerPublic)}},
		Requirements: []Requirement{{
			ID: "vastplan-stable", Channel: "stable", Publisher: "vastplan", PluginPrefix: "cn.vastplan.", ProviderIDs: []string{record.ProviderID},
			BuilderIDs: []string{summary.BuilderID}, BuildTypes: []string{summary.BuildType}, SourceURIPrefixes: []string{"git+https://github.com/yandt/VastPlan"},
			Issuers: []string{record.Issuer}, Workflows: []string{record.Workflow}, RequireSourceDigest: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := ArtifactIdentity{PluginID: "cn.vastplan.example", Channel: "stable", Publisher: "vastplan", SHA256: artifactSHA}
	verified, err := verifier.Verify(identity, provenance, recordRaw, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if verified.SubjectSHA256 != artifactSHA || verified.PolicyID != "vastplan-stable" {
		t.Fatalf("验证结果未绑定制品与策略: %+v", verified)
	}

	t.Run("tampered provenance", func(t *testing.T) {
		mutated := append([]byte(nil), provenance...)
		mutated[len(mutated)-2] ^= 1
		if _, err := verifier.Verify(identity, mutated, recordRaw, now.Add(time.Hour)); err == nil {
			t.Fatal("原始 Provenance 被替换必须拒绝")
		}
	})
	t.Run("wrong package", func(t *testing.T) {
		wrong := identity
		wrong.SHA256 = hex.EncodeToString(make([]byte, sha256.Size))
		if _, err := verifier.Verify(wrong, provenance, recordRaw, now.Add(time.Hour)); err == nil {
			t.Fatal("Record 不能用于另一插件包")
		}
	})
	t.Run("expired record", func(t *testing.T) {
		if _, err := verifier.Verify(identity, provenance, recordRaw, now.Add(25*time.Hour)); err == nil {
			t.Fatal("过期 Verification Record 必须拒绝")
		}
	})
}

func TestVerifierOptionalChannelAndPartialEvidence(t *testing.T) {
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	verifier, err := NewVerifier(TrustPolicy{
		RequiredChannels: []string{}, MaxRecordTTLHours: 24,
		Keys:         []VerifierKey{{ProviderID: "provider", KeyID: "key", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}},
		Requirements: []Requirement{{ID: "testing", Channel: "testing", ProviderIDs: []string{"provider"}, BuilderIDs: []string{"builder"}, BuildTypes: []string{"build"}, SourceURIPrefixes: []string{"git+https://"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	identity := ArtifactIdentity{PluginID: "cn.example.plugin", Publisher: "example", Channel: "testing", SHA256: hex.EncodeToString(make([]byte, sha256.Size))}
	if record, err := verifier.Verify(identity, nil, nil, time.Now().UTC()); err != nil || record != nil {
		t.Fatalf("非强制 channel 可省略整组证据: record=%v err=%v", record, err)
	}
	if _, err := verifier.Verify(identity, []byte("{}"), nil, time.Now().UTC()); err == nil {
		t.Fatal("只提供一半 sidecar 必须拒绝")
	}
}

func signedTestDSSE(t *testing.T, artifactSHA, keyID string, privateKey ed25519.PrivateKey) []byte {
	t.Helper()
	statement := map[string]any{
		"_type":         InTotoStatementType,
		"subject":       []any{map[string]any{"name": "plugin.tar.gz", "digest": map[string]string{"sha256": artifactSHA}}},
		"predicateType": SLSAProvenanceType,
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType":            "https://vastplan.example/build/plugin/v1",
				"resolvedDependencies": []any{map[string]any{"uri": "git+https://github.com/yandt/VastPlan@refs/heads/main", "digest": map[string]string{"gitCommit": "0123456789abcdef"}}},
			},
			"runDetails": map[string]any{"builder": map[string]string{"id": "https://builder.example.com/plugin-release"}},
		},
	}
	payload, err := json.Marshal(statement)
	if err != nil {
		t.Fatal(err)
	}
	envelope := dsseEnvelope{PayloadType: DSSEPayloadType, Payload: base64.StdEncoding.EncodeToString(payload)}
	envelope.Signatures = []dsseSignature{{KeyID: keyID, Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, pae(envelope.PayloadType, payload)))}}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
