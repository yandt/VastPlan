package artifactassessment

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

func TestAdmissionAndAppendOnlyStatus(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 24, 2, 0, 0, 0, time.UTC)
	identity := ArtifactIdentity{PluginID: "cn.example.plugin", Channel: "stable", Publisher: "example", SHA256: testDigest("artifact"), SBOMSHA256: testDigest("sbom")}
	zero := uint64(0)
	verifier, err := NewVerifier(TrustPolicy{
		Keys:         []ProviderKey{{ProviderID: "security.enterprise", KeyID: "release", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}},
		Requirements: []Requirement{{ID: "stable-default", Channel: "stable", Publisher: "example", PluginPrefix: "cn.example.", ProviderIDs: []string{"security.enterprise"}, ScannerIDs: []string{"scanner.test"}, Maximum: MaximumFindings{Critical: &zero, High: &zero, DeniedLicense: &zero, UnknownLicense: &zero}, RequireReportDigests: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	evaluation := Evaluation{SubjectSHA256: identity.SHA256, SBOMSHA256: identity.SBOMSHA256, Scanner: Scanner{ID: "scanner.test", Version: "1.0.0", DatabaseRevision: "2026-07-24"}, Vulnerabilities: VulnerabilitySummary{ReportSHA256: testDigest("vulnerability report")}, Licenses: LicenseSummary{Allowed: 1, ReportSHA256: testDigest("license report")}, Decision: DecisionPass, EvaluatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	admission, err := SignAdmission(AdmissionRecord{Evaluation: evaluation, ProviderID: "security.enterprise", KeyID: "release", PolicyID: "stable-default"}, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	admissionRaw, _ := json.Marshal(admission)
	_, admissionDigest, err := verifier.VerifyAdmission(identity, admissionRaw, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	status, err := SignStatus(StatusRecord{AdmissionSHA256: admissionDigest, Sequence: 1, PreviousSHA256: admissionDigest, Evaluation: evaluation, ProviderID: "security.enterprise", KeyID: "release", PolicyID: "stable-default"}, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	statusRaw, _ := json.Marshal(status)
	if _, _, err := verifier.VerifyStatus(identity, admissionRaw, nil, statusRaw, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	status.Sequence = 2
	status.PreviousSHA256 = testDigest("rollback")
	status, _ = SignStatus(status, privateKey)
	statusRaw, _ = json.Marshal(status)
	if _, _, err := verifier.VerifyStatus(identity, admissionRaw, []byte(`{}`), statusRaw, now.Add(time.Hour)); err == nil {
		t.Fatal("expected broken status chain to be rejected")
	}
}

func TestPolicyRejectsFindingsAndExpiredRecord(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 24, 2, 0, 0, 0, time.UTC)
	identity := ArtifactIdentity{PluginID: "cn.example.plugin", Channel: "stable", Publisher: "example", SHA256: testDigest("artifact"), SBOMSHA256: testDigest("sbom")}
	zero := uint64(0)
	verifier, err := NewVerifier(TrustPolicy{Keys: []ProviderKey{{ProviderID: "provider", KeyID: "key", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}, Requirements: []Requirement{{ID: "stable", Channel: "stable", ProviderIDs: []string{"provider"}, ScannerIDs: []string{"scanner"}, Maximum: MaximumFindings{Critical: &zero}}}})
	if err != nil {
		t.Fatal(err)
	}
	evaluation := Evaluation{SubjectSHA256: identity.SHA256, SBOMSHA256: identity.SBOMSHA256, Scanner: Scanner{ID: "scanner", Version: "1", DatabaseRevision: "db"}, Vulnerabilities: VulnerabilitySummary{Critical: 1}, Decision: DecisionPass, EvaluatedAt: now, ExpiresAt: now.Add(time.Hour)}
	record, _ := SignAdmission(AdmissionRecord{Evaluation: evaluation, ProviderID: "provider", KeyID: "key", PolicyID: "stable"}, privateKey)
	raw, _ := json.Marshal(record)
	if _, _, err := verifier.VerifyAdmission(identity, raw, now.Add(time.Minute)); err == nil {
		t.Fatal("expected critical finding to be rejected")
	}
	evaluation.Vulnerabilities.Critical = 0
	record, _ = SignAdmission(AdmissionRecord{Evaluation: evaluation, ProviderID: "provider", KeyID: "key", PolicyID: "stable"}, privateKey)
	raw, _ = json.Marshal(record)
	if _, _, err := verifier.VerifyAdmission(identity, raw, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expected expired assessment to be rejected")
	}
}

func TestFailedRescanIsTrustedEvidenceButCannotBeEnforced(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 7, 24, 2, 0, 0, 0, time.UTC)
	identity := ArtifactIdentity{PluginID: "cn.example.plugin", Channel: "stable", Publisher: "example", SHA256: testDigest("artifact"), SBOMSHA256: testDigest("sbom")}
	zero := uint64(0)
	verifier, err := NewVerifier(TrustPolicy{Keys: []ProviderKey{{ProviderID: "provider", KeyID: "key", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}, Requirements: []Requirement{{ID: "stable", Channel: "stable", ProviderIDs: []string{"provider"}, ScannerIDs: []string{"scanner"}, Maximum: MaximumFindings{Critical: &zero}}}})
	if err != nil {
		t.Fatal(err)
	}
	clean := Evaluation{SubjectSHA256: identity.SHA256, SBOMSHA256: identity.SBOMSHA256, Scanner: Scanner{ID: "scanner", Version: "1", DatabaseRevision: "db-1"}, Decision: DecisionPass, EvaluatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	admission, _ := SignAdmission(AdmissionRecord{Evaluation: clean, ProviderID: "provider", KeyID: "key", PolicyID: "stable"}, privateKey)
	admissionRaw, _ := json.Marshal(admission)
	_, admissionDigest, err := verifier.VerifyAdmission(identity, admissionRaw, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	failed := clean
	failed.Scanner.DatabaseRevision = "db-2"
	failed.Vulnerabilities.Critical = 1
	failed.Decision = DecisionFail
	status, _ := SignStatus(StatusRecord{AdmissionSHA256: admissionDigest, Sequence: 1, PreviousSHA256: admissionDigest, Evaluation: failed, ProviderID: "provider", KeyID: "key", PolicyID: "stable"}, privateKey)
	statusRaw, _ := json.Marshal(status)
	verified, _, err := verifier.VerifyStatus(identity, admissionRaw, nil, statusRaw, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := EnforceDecision(verified.Evaluation); err == nil {
		t.Fatal("expected latest failed rescan to block install")
	}
}

func TestFreshRescanExtendsExpiredAdmissionWithoutRewritingIt(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	issued := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	now := issued.Add(48 * time.Hour)
	identity := ArtifactIdentity{PluginID: "cn.example.plugin", Channel: "stable", Publisher: "example", SHA256: testDigest("artifact"), SBOMSHA256: testDigest("sbom")}
	verifier, err := NewVerifier(TrustPolicy{MaxRecordTTLHours: 168, Keys: []ProviderKey{{ProviderID: "provider", KeyID: "key", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}}, Requirements: []Requirement{{ID: "stable", Channel: "stable", ProviderIDs: []string{"provider"}, ScannerIDs: []string{"scanner"}}}})
	if err != nil {
		t.Fatal(err)
	}
	base := Evaluation{SubjectSHA256: identity.SHA256, SBOMSHA256: identity.SBOMSHA256, Scanner: Scanner{ID: "scanner", Version: "1", DatabaseRevision: "db-1"}, Decision: DecisionPass, EvaluatedAt: issued, ExpiresAt: issued.Add(time.Hour)}
	admission, _ := SignAdmission(AdmissionRecord{Evaluation: base, ProviderID: "provider", KeyID: "key", PolicyID: "stable"}, privateKey)
	admissionRaw, _ := json.Marshal(admission)
	if _, _, err := verifier.VerifyAdmission(identity, admissionRaw, now); err == nil {
		t.Fatal("expired admission must not remain the current operational state")
	}
	_, admissionDigest, _ := InspectAdmission(admissionRaw)
	fresh := base
	fresh.Scanner.DatabaseRevision = "db-2"
	fresh.EvaluatedAt, fresh.ExpiresAt = now.Add(-time.Hour), now.Add(24*time.Hour)
	status, _ := SignStatus(StatusRecord{AdmissionSHA256: admissionDigest, Sequence: 1, PreviousSHA256: admissionDigest, Evaluation: fresh, ProviderID: "provider", KeyID: "key", PolicyID: "stable"}, privateKey)
	statusRaw, _ := json.Marshal(status)
	if _, _, err := verifier.VerifyStatus(identity, admissionRaw, nil, statusRaw, now); err != nil {
		t.Fatalf("fresh rescan should extend the immutable admission root: %v", err)
	}
}

func testDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
