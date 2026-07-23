package controller

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
)

type memoryPlanEntry struct {
	plan     Plan
	revision uint64
}
type memoryPlanStore struct {
	mu     sync.Mutex
	values map[string]memoryPlanEntry
}

func newMemoryPlanStore() *memoryPlanStore {
	return &memoryPlanStore{values: map[string]memoryPlanEntry{}}
}
func (s *memoryPlanStore) Load(_ context.Context, _ *contractv1.CallContext, key string) (Plan, uint64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	return value.plan, value.revision, ok, nil
}
func (s *memoryPlanStore) Save(_ context.Context, _ *contractv1.CallContext, key string, plan Plan, expected uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.values[key]
	if !exists && expected != 0 || exists && current.revision != expected {
		return 0, errors.New("conflict")
	}
	current = memoryPlanEntry{plan: plan, revision: current.revision + 1}
	s.values[key] = current
	return current.revision, nil
}

type fakePorts struct {
	entry                             platformadminapi.ArtifactCatalogEntry
	status                            []byte
	providerStatus                    artifactassessment.ProviderRuntimeStatus
	providerErr, assessErr, appendErr error
	assessed, appended                int
}

func (p *fakePorts) ProviderStatus(context.Context, *contractv1.CallContext) (artifactassessment.ProviderRuntimeStatus, error) {
	if p.providerErr != nil {
		return artifactassessment.ProviderRuntimeStatus{}, p.providerErr
	}
	if p.providerStatus.SchemaVersion != "" {
		return p.providerStatus, nil
	}
	return defaultProviderStatus(), nil
}

func defaultProviderStatus() artifactassessment.ProviderRuntimeStatus {
	return artifactassessment.ProviderRuntimeStatus{SchemaVersion: artifactassessment.SchemaVersion, Scanner: artifactassessment.Scanner{ID: "trivy.filesystem", Version: "1", DatabaseRevision: strings.Repeat("e", 64)}, AssessmentRevision: strings.Repeat("9", 64)}
}

func (p *fakePorts) ListCatalog(context.Context, *contractv1.CallContext, string, int, int) (platformadminapi.ArtifactCatalogPage, error) {
	return platformadminapi.ArtifactCatalogPage{Revision: 7, Total: 1, Page: 1, PageSize: 100, Items: []platformadminapi.ArtifactCatalogEntry{p.entry}}, nil
}
func (p *fakePorts) AssessStatus(_ context.Context, _ *contractv1.CallContext, request artifactassessment.ProviderStatusRequest) ([]byte, error) {
	p.assessed++
	if p.assessErr != nil {
		return nil, p.assessErr
	}
	record, _, _ := artifactassessment.InspectStatus(p.status)
	if record.Sequence != request.Sequence || record.PreviousSHA256 != request.PreviousSHA256 {
		return nil, errors.New("request mismatch")
	}
	return append([]byte(nil), p.status...), nil
}
func (p *fakePorts) AppendStatus(_ context.Context, _ *contractv1.CallContext, request artifactassessment.AppendStatusRequest) error {
	p.appended++
	if p.appendErr != nil {
		return p.appendErr
	}
	if len(request.Record) == 0 {
		return errors.New("empty")
	}
	return nil
}

func TestReconcilePersistsPendingBeforeAppendAndThenDefers(t *testing.T) {
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	entry := controllerEntry(now.Add(time.Hour))
	status := signedStatus(t, entry, 1, entry.SecurityAdmission.AdmissionSHA256, now, now.Add(24*time.Hour))
	ports := &fakePorts{entry: entry, status: status}
	store := newMemoryPlanStore()
	controller := testController(t, ports, store, now)
	stats, err := controller.ReconcileOnce(context.Background())
	if err != nil || stats.Succeeded != 1 || ports.assessed != 1 || ports.appended != 1 {
		t.Fatalf("首轮复扫未收敛: stats=%+v err=%v", stats, err)
	}
	plan, _, ok, _ := store.Load(context.Background(), nil, planKey(entry.Ref))
	if !ok || plan.LastSequence != 1 || len(plan.PendingRecord) != 0 || plan.Attempts != 0 || !plan.NextScanAt.After(now) {
		t.Fatalf("成功计划无效: %+v", plan)
	}
	stats, err = controller.ReconcileOnce(context.Background())
	if err != nil || stats.Deferred != 1 || ports.assessed != 1 || ports.appended != 1 {
		t.Fatalf("未到期计划不应重复扫描: %+v err=%v", stats, err)
	}
}

func TestReconcileRetriesPendingRecordWithoutRescanning(t *testing.T) {
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	entry := controllerEntry(now.Add(time.Hour))
	status := signedStatus(t, entry, 1, entry.SecurityAdmission.AdmissionSHA256, now, now.Add(24*time.Hour))
	ports := &fakePorts{entry: entry, status: status, appendErr: errors.New("temporary")}
	store := newMemoryPlanStore()
	controller := testController(t, ports, store, now)
	stats, _ := controller.ReconcileOnce(context.Background())
	if stats.Failed != 1 || ports.assessed != 1 || ports.appended != 1 {
		t.Fatalf("失败状态未记录: %+v", stats)
	}
	plan, _, _, _ := store.Load(context.Background(), nil, planKey(entry.Ref))
	if len(plan.PendingRecord) == 0 || plan.Attempts != 1 || !plan.NextScanAt.After(now) {
		t.Fatalf("pending/退避未持久化: %+v", plan)
	}
	ports.appendErr = nil
	controller.now = func() time.Time { return plan.NextScanAt.Add(time.Second) }
	stats, _ = controller.ReconcileOnce(context.Background())
	if stats.Succeeded != 1 || ports.assessed != 1 || ports.appended != 2 {
		t.Fatalf("重试必须复用 pending record: %+v", stats)
	}
}

type failSavePlanStore struct {
	*memoryPlanStore
	failAt int
	saves  int
}

func (s *failSavePlanStore) Save(ctx context.Context, call *contractv1.CallContext, key string, plan Plan, expected uint64) (uint64, error) {
	s.saves++
	if s.saves == s.failAt {
		return 0, errors.New("injected save failure")
	}
	return s.memoryPlanStore.Save(ctx, call, key, plan, expected)
}

func TestReconcileRecoversWhenAppendSucceededButFinalPlanSaveFailed(t *testing.T) {
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	entry := controllerEntry(now.Add(time.Hour))
	statusRaw := signedStatus(t, entry, 1, entry.SecurityAdmission.AdmissionSHA256, now, now.Add(24*time.Hour))
	status, digest, err := artifactassessment.InspectStatus(statusRaw)
	if err != nil {
		t.Fatal(err)
	}
	ports := &fakePorts{entry: entry, status: statusRaw}
	store := &failSavePlanStore{memoryPlanStore: newMemoryPlanStore(), failAt: 3}
	controller := testController(t, ports, store, now)
	stats, err := controller.ReconcileOnce(context.Background())
	if err != nil || stats.Conflicts != 1 || ports.assessed != 1 || ports.appended != 1 {
		t.Fatalf("最终计划保存故障应保留已追加事实: stats=%+v err=%v", stats, err)
	}
	plan, _, _, _ := store.Load(context.Background(), nil, planKey(entry.Ref))
	if len(plan.PendingRecord) == 0 {
		t.Fatal("最终保存失败时必须保留 append 前持久化的 pending record")
	}
	ports.entry.SecurityStatus = &platformadminapi.ArtifactSecurityStatusEvidence{
		Sequence: status.Sequence, RecordSHA256: digest, PreviousSHA256: status.PreviousSHA256,
		Decision: status.Evaluation.Decision, DatabaseRevision: status.Evaluation.Scanner.DatabaseRevision,
		EvaluatedAt: status.Evaluation.EvaluatedAt.Format(time.RFC3339Nano), ExpiresAt: status.Evaluation.ExpiresAt.Format(time.RFC3339Nano), Verification: "verified",
	}
	stats, err = controller.ReconcileOnce(context.Background())
	if err != nil || stats.Deferred != 1 || ports.assessed != 1 || ports.appended != 1 {
		t.Fatalf("Catalog 链头应清除 pending 且不得重放: stats=%+v err=%v assessed=%d appended=%d", stats, err, ports.assessed, ports.appended)
	}
	plan, _, _, _ = store.Load(context.Background(), nil, planKey(entry.Ref))
	if len(plan.PendingRecord) != 0 || plan.LastSequence != 1 || plan.LastRecordSHA256 != digest {
		t.Fatalf("恢复后的计划链头无效: %+v", plan)
	}
}

func TestProviderDatabaseRevisionChangeTriggersImmediateNextSequence(t *testing.T) {
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	entry := controllerEntry(now.Add(time.Hour))
	firstRaw := signedStatus(t, entry, 1, entry.SecurityAdmission.AdmissionSHA256, now, now.Add(24*time.Hour))
	ports := &fakePorts{entry: entry, status: firstRaw}
	store := newMemoryPlanStore()
	controller := testController(t, ports, store, now)
	if stats, err := controller.ReconcileOnce(context.Background()); err != nil || stats.Succeeded != 1 {
		t.Fatalf("首轮复扫失败: stats=%+v err=%v", stats, err)
	}
	_, firstDigest, err := artifactassessment.InspectStatus(firstRaw)
	if err != nil {
		t.Fatal(err)
	}
	ports.providerStatus = defaultProviderStatus()
	ports.providerStatus.Scanner.DatabaseRevision = "db-revision-2"
	ports.providerStatus.AssessmentRevision = strings.Repeat("8", 64)
	ports.status = signedStatusWithDatabase(t, entry, 2, firstDigest, now.Add(time.Minute), now.Add(25*time.Hour), "db-revision-2")
	controller.now = func() time.Time { return now.Add(time.Minute) }
	stats, err := controller.ReconcileOnce(context.Background())
	if err != nil || stats.Succeeded != 1 || ports.assessed != 2 || ports.appended != 2 {
		t.Fatalf("数据库 revision 变化必须立即产生下一 sequence: stats=%+v err=%v", stats, err)
	}
	plan, _, _, _ := store.Load(context.Background(), nil, planKey(entry.Ref))
	if plan.LastSequence != 2 || plan.DatabaseRevision != "db-revision-2" || plan.AssessmentRevision != strings.Repeat("8", 64) {
		t.Fatalf("新数据库计划未收敛: %+v", plan)
	}
}

func testController(t *testing.T, ports Ports, store PlanStore, now time.Time) *Controller {
	t.Helper()
	config := Config{TenantID: "tenant-a", Channels: []string{"stable"}, IntervalSeconds: 60, LeadTimeSeconds: 7200, JitterSeconds: 600, RetryBaseSeconds: 30, RetryMaxSeconds: 3600, PageSize: 100}
	controller, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	controller.ports, controller.store, controller.now = ports, store, func() time.Time { return now }
	return controller
}

func controllerEntry(expires time.Time) platformadminapi.ArtifactCatalogEntry {
	return platformadminapi.ArtifactCatalogEntry{
		Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.demo", Version: "1.0.0", Channel: "stable"}, SHA256: strings.Repeat("a", 64), Publisher: "vastplan", LifecycleStatus: "active",
		SBOM:              &platformadminapi.ArtifactSBOMDeclaration{Format: "cyclonedx-json", SpecVersion: "1.5", SHA256: strings.Repeat("b", 64)},
		SecurityAdmission: &platformadminapi.ArtifactSecurityAdmissionDeclaration{AdmissionSHA256: strings.Repeat("c", 64), ProviderID: "security.vastplan", KeyID: "release", PolicyID: "stable-default", ScannerID: "trivy.filesystem", ScannerVersion: "1", DatabaseRevision: strings.Repeat("d", 64), Decision: "pass", EvaluatedAt: expires.Add(-24 * time.Hour).Format(time.RFC3339Nano), ExpiresAt: expires.Format(time.RFC3339Nano)},
	}
}

func signedStatus(t *testing.T, entry platformadminapi.ArtifactCatalogEntry, sequence uint64, previous string, evaluatedAt, expiresAt time.Time) []byte {
	return signedStatusWithDatabase(t, entry, sequence, previous, evaluatedAt, expiresAt, strings.Repeat("e", 64))
}

func signedStatusWithDatabase(t *testing.T, entry platformadminapi.ArtifactCatalogEntry, sequence uint64, previous string, evaluatedAt, expiresAt time.Time, databaseRevision string) []byte {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	record, err := artifactassessment.SignStatus(artifactassessment.StatusRecord{AdmissionSHA256: entry.SecurityAdmission.AdmissionSHA256, Sequence: sequence, PreviousSHA256: previous,
		Evaluation: artifactassessment.Evaluation{SubjectSHA256: entry.SHA256, SBOMSHA256: entry.SBOM.SHA256, Scanner: artifactassessment.Scanner{ID: "trivy.filesystem", Version: "1", DatabaseRevision: databaseRevision}, Vulnerabilities: artifactassessment.VulnerabilitySummary{ReportSHA256: strings.Repeat("f", 64)}, Licenses: artifactassessment.LicenseSummary{ReportSHA256: strings.Repeat("f", 64)}, Decision: artifactassessment.DecisionPass, EvaluatedAt: evaluatedAt, ExpiresAt: expiresAt}, ProviderID: "security.vastplan", KeyID: "release", PolicyID: entry.SecurityAdmission.PolicyID}, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(record)
	return raw
}
