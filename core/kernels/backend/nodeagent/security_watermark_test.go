package nodeagent

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

func TestSecurityWatermarkRejectsRepositoryRollback(t *testing.T) {
	_, privateKey, _ := ed25519.GenerateKey(nil)
	artifactSHA, sbomSHA := strings.Repeat("a", 64), strings.Repeat("b", 64)
	now := time.Now().UTC().Add(-time.Hour)
	evaluation := artifactassessment.Evaluation{SubjectSHA256: artifactSHA, SBOMSHA256: sbomSHA, Scanner: artifactassessment.Scanner{ID: "scanner", Version: "1", DatabaseRevision: "db"}, Decision: artifactassessment.DecisionPass, EvaluatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	admission, _ := artifactassessment.SignAdmission(artifactassessment.AdmissionRecord{Evaluation: evaluation, ProviderID: "provider", KeyID: "key", PolicyID: "policy"}, privateKey)
	admissionRaw, _ := json.Marshal(admission)
	_, admissionDigest, _ := artifactassessment.InspectAdmission(admissionRaw)
	first, _ := artifactassessment.SignStatus(artifactassessment.StatusRecord{AdmissionSHA256: admissionDigest, Sequence: 1, PreviousSHA256: admissionDigest, Evaluation: evaluation, ProviderID: "provider", KeyID: "key", PolicyID: "policy"}, privateKey)
	firstRaw, _ := json.Marshal(first)
	_, firstDigest, _ := artifactassessment.InspectStatus(firstRaw)
	second := first
	second.Sequence, second.PreviousSHA256, second.Evaluation.Scanner.DatabaseRevision = 2, firstDigest, "db-2"
	second, _ = artifactassessment.SignStatus(second, privateKey)
	secondRaw, _ := json.Marshal(second)
	secondChain, _ := artifactassessment.MarshalStatusChain([][]byte{firstRaw, secondRaw})
	firstChain, _ := artifactassessment.MarshalStatusChain([][]byte{firstRaw})
	root := t.TempDir()
	newer := VerifiedArtifact{artifact: pluginv1.Artifact{SHA256: artifactSHA}, securityAdmission: admissionRaw, securityStatusChain: secondChain}
	if err := enforceSecurityWatermark(root, newer); err != nil {
		t.Fatal(err)
	}
	rollback := VerifiedArtifact{artifact: pluginv1.Artifact{SHA256: artifactSHA}, securityAdmission: admissionRaw, securityStatusChain: firstChain}
	if err := enforceSecurityWatermark(root, rollback); err == nil {
		t.Fatal("repository rollback to an older signed status chain must be rejected")
	}
	if err := enforceSecurityWatermark(root, newer); err != nil {
		t.Fatalf("same high watermark must be idempotent: %v", err)
	}
}
