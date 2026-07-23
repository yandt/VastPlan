package provenanceprovider

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactprovenance"
)

func TestStaticProviderVerifiesBuilderAndSignsRecord(t *testing.T) {
	builderPublic, builderPrivate, _ := ed25519.GenerateKey(rand.Reader)
	providerPublic, providerPrivate, _ := ed25519.GenerateKey(rand.Reader)
	digest := sha256.Sum256([]byte("plugin"))
	subject := hex.EncodeToString(digest[:])
	provenance := testProvenance(t, subject, builderPrivate)
	now := time.Date(2026, 7, 24, 2, 0, 0, 0, time.UTC)
	record, err := Verify(Options{
		SubjectSHA256: subject, Provenance: provenance, Now: now,
		BuilderTrust: BuilderTrustDocument{SchemaVersion: "v1", Keys: []BuilderKey{{KeyID: "builder", PublicKey: base64.StdEncoding.EncodeToString(builderPublic)}}},
		Policy:       Policy{SchemaVersion: "v1", ID: "release", BuilderIDs: []string{"builder-id"}, BuildTypes: []string{"build-type"}, SourceURIPrefixes: []string{"git+https://example.com/"}, RequireSourceDigest: true, RecordTTLHours: 24},
		ProviderID:   "provider.static", ProviderKeyID: "provider-key", ProviderKey: providerPrivate,
	})
	if err != nil {
		t.Fatal(err)
	}
	trust, err := artifactprovenance.NewVerifier(artifactprovenance.TrustPolicy{
		RequiredChannels: []string{"stable"}, MaxRecordTTLHours: 24,
		Keys:         []artifactprovenance.VerifierKey{{ProviderID: record.ProviderID, KeyID: record.KeyID, PublicKey: base64.StdEncoding.EncodeToString(providerPublic)}},
		Requirements: []artifactprovenance.Requirement{{ID: "release", Channel: "stable", ProviderIDs: []string{record.ProviderID}, BuilderIDs: []string{"builder-id"}, BuildTypes: []string{"build-type"}, SourceURIPrefixes: []string{"git+https://example.com/"}, RequireSourceDigest: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(record)
	if _, err := trust.Verify(artifactprovenance.ArtifactIdentity{PluginID: "cn.example.plugin", Publisher: "example", Channel: "stable", SHA256: subject}, provenance, raw, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func testProvenance(t *testing.T, subject string, privateKey ed25519.PrivateKey) []byte {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"_type":         artifactprovenance.InTotoStatementType,
		"subject":       []any{map[string]any{"name": "plugin.tar.gz", "digest": map[string]string{"sha256": subject}}},
		"predicateType": artifactprovenance.SLSAProvenanceType,
		"predicate": map[string]any{
			"buildDefinition": map[string]any{"buildType": "build-type", "resolvedDependencies": []any{map[string]any{"uri": "git+https://example.com/repo", "digest": map[string]string{"gitCommit": "abc"}}}},
			"runDetails":      map[string]any{"builder": map[string]string{"id": "builder-id"}},
		},
	})
	payloadType := artifactprovenance.DSSEPayloadType
	pae := []byte(fmt.Sprintf("DSSEv1 %d %s %d %s", len(payloadType), payloadType, len(payload), payload))
	raw, _ := json.Marshal(map[string]any{"payloadType": payloadType, "payload": base64.StdEncoding.EncodeToString(payload), "signatures": []any{map[string]string{"keyid": "builder", "sig": base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, pae))}}})
	return raw
}
