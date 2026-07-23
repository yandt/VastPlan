package repositoryruntime

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreport"
)

func TestRepositoryRequiresArchivedReportsBeforeAcceptingAssessmentEvidence(t *testing.T) {
	archive, err := artifactreport.New(filepath.Join(t.TempDir(), "reports"))
	if err != nil {
		t.Fatal(err)
	}
	report := []byte(`{"SchemaVersion":2,"Results":[]}`)
	digestBytes := sha256.Sum256(report)
	digest := hex.EncodeToString(digestBytes[:])
	evaluation := artifactassessment.Evaluation{
		SubjectSHA256: strings.Repeat("a", 64), SBOMSHA256: strings.Repeat("b", 64),
		Scanner:         artifactassessment.Scanner{ID: "fixture", Version: "1", DatabaseRevision: strings.Repeat("c", 64)},
		Vulnerabilities: artifactassessment.VulnerabilitySummary{ReportSHA256: digest},
		Licenses:        artifactassessment.LicenseSummary{ReportSHA256: digest}, Decision: artifactassessment.DecisionPass,
		EvaluatedAt: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := artifactassessment.SignAdmission(artifactassessment.AdmissionRecord{Evaluation: evaluation, ProviderID: "fixture", KeyID: "key", PolicyID: "policy"}, key)
	if err != nil {
		t.Fatal(err)
	}
	admissionRaw, _ := json.Marshal(admission)
	manager := &Manager{assessmentReports: archive}
	if err := manager.requireAdmissionReports(admissionRaw); err == nil {
		t.Fatal("报告尚未归档时必须拒绝 AdmissionRecord")
	}
	if err := archive.Put(digest, report); err != nil {
		t.Fatal(err)
	}
	if err := manager.requireAdmissionReports(admissionRaw); err != nil {
		t.Fatalf("报告归档后 AdmissionRecord 应通过: %v", err)
	}
	status, err := artifactassessment.SignStatus(artifactassessment.StatusRecord{
		AdmissionSHA256: strings.Repeat("d", 64), Sequence: 1, PreviousSHA256: strings.Repeat("d", 64),
		Evaluation: evaluation, ProviderID: "fixture", KeyID: "key", PolicyID: "policy",
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	statusRaw, _ := json.Marshal(status)
	if err := manager.requireStatusReports(statusRaw); err != nil {
		t.Fatalf("已归档 StatusRecord 应通过: %v", err)
	}
	if err := (&Manager{}).requireStatusReports(statusRaw); err == nil {
		t.Fatal("Repository 未配置归档时不得接受带报告引用的 StatusRecord")
	}
}
