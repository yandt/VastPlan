package credentials

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

func auditContext(tenant string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant, Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "platform-admin"}}
}

func TestMaintenanceConfigurationDefaultsAndRejectsUnsafeValues(t *testing.T) {
	policy, err := (Configuration{}).Policy()
	if err != nil {
		t.Fatal(err)
	}
	if policy.PreparingMaxAge != defaultPreparingMaxAge || policy.AbortedRetention != defaultAbortedRetention || policy.AuditRetention != defaultAuditRetention || policy.Interval != defaultMaintenancePeriod || policy.BatchSize != defaultMaintenanceBatch || policy.OrphanChunkGrace != defaultOrphanChunkGrace || policy.ChunkGCBatchSize != defaultChunkGCBatch {
		t.Fatalf("默认维护策略不一致: %+v", policy)
	}
	invalid := []Configuration{
		{Maintenance: MaintenanceConfiguration{PreparingMaxAgeSeconds: -1}},
		{Maintenance: MaintenanceConfiguration{IntervalSeconds: math.MaxInt64}},
		{Maintenance: MaintenanceConfiguration{AbortedRetentionSeconds: 7200, AuditRetentionSeconds: 3600}},
		{Maintenance: MaintenanceConfiguration{BatchSize: 1001}},
		{Maintenance: MaintenanceConfiguration{OrphanChunkGraceSeconds: 3599}},
		{Maintenance: MaintenanceConfiguration{ChunkGCBatchSize: 201}},
	}
	for _, configuration := range invalid {
		if _, err := configuration.Policy(); err == nil {
			t.Fatalf("不安全维护配置必须拒绝: %+v", configuration)
		}
	}
}

func TestManagedMaintenanceCollectsOnlyExpiredNonRunnableRecordsAndPersistsAudit(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	policy := MaintenancePolicy{PreparingMaxAge: time.Hour, AbortedRetention: 2 * time.Hour, AuditRetention: 4 * time.Hour, Interval: time.Hour, BatchSize: 20}
	path := filepath.Join(t.TempDir(), "credentials.json")
	service, err := openTestServiceWithOptions(path, &fakeTransit{}, ServiceOptions{Maintenance: policy, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	record := func(id, state string, updated time.Time) ManagedRecord {
		ciphertext := "vault:v1:secret-" + id
		if state == managedAborted || state == managedRetired {
			ciphertext = ""
		}
		return ManagedRecord{
			StageID:  id,
			Ref:      pluginconfig.ManagedCredentialRef{Handle: "credential://managed/" + strings.TrimPrefix(id, "stage-"), Scope: "tenant", Owner: "plugin.database", Purpose: "database.connection", Version: 1},
			Resource: "primary", State: state, CreatedAt: updated.Add(-time.Hour), UpdatedAt: updated, Ciphertext: ciphertext,
		}
	}
	service.data.Managed["tenant-a"] = map[string]ManagedRecord{
		"stage-preparing-old": record("stage-preparing-old", managedPreparing, now.Add(-90*time.Minute)),
		"stage-preparing-new": record("stage-preparing-new", managedPreparing, now.Add(-30*time.Minute)),
		"stage-aborted-old":   record("stage-aborted-old", managedAborted, now.Add(-3*time.Hour)),
		"stage-candidate-old": record("stage-candidate-old", managedCandidate, now.Add(-72*time.Hour)),
		"stage-active-old":    record("stage-active-old", managedActive, now.Add(-72*time.Hour)),
		"stage-retired-old":   record("stage-retired-old", managedRetired, now.Add(-72*time.Hour)),
	}
	service.data.ManagedAudit["tenant-a"] = managedAuditState{NextID: 1, Events: []ManagedAuditEvent{{
		ID: 1, CredentialFingerprint: strings.Repeat("a", 32), Action: "managed.staged", State: managedPreparing,
		Owner: "plugin.database", Purpose: "database.connection", Resource: "primary", OccurredAt: now.Add(-5 * time.Hour),
	}}}
	if err := service.save(); err != nil {
		t.Fatal(err)
	}
	if err := service.CollectExpiredManaged(); err != nil {
		t.Fatal(err)
	}
	records := service.data.Managed["tenant-a"]
	if _, exists := records["stage-aborted-old"]; exists {
		t.Fatal("超过保留期的 Aborted 记录应被删除")
	}
	autoAborted := records["stage-preparing-old"]
	if autoAborted.State != managedAborted || autoAborted.Ciphertext != "" {
		t.Fatalf("过期 Preparing 必须终止并立即清除密文: %+v", autoAborted)
	}
	for _, id := range []string{"stage-preparing-new", "stage-candidate-old", "stage-active-old", "stage-retired-old"} {
		if _, exists := records[id]; !exists {
			t.Fatalf("可运行或需保留取证的记录不得按时间删除: %s", id)
		}
	}
	page, err := service.ListManagedAudit(auditContext("tenant-a"), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].Action != "managed.auto-aborted" || page.Items[1].Action != "managed.collected" {
		t.Fatalf("维护审计事件或保留期清理不正确: %+v", page.Items)
	}
	if page.Maintenance.AutoAborted != 1 || page.Maintenance.Collected != 1 || page.Maintenance.Counts[managedActive] != 1 {
		t.Fatalf("维护状态不正确: %+v", page.Maintenance)
	}
	raw, _ := json.Marshal(page)
	for _, forbidden := range []string{"credential://managed/", "stage-", "vault:v1:", "secret-", "authority_"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("脱敏审计泄露 %q: %s", forbidden, raw)
		}
	}
	reopened, err := openTestServiceWithOptions(path, &fakeTransit{}, ServiceOptions{Maintenance: policy, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	reopenedPage, err := reopened.ListManagedAudit(auditContext("tenant-a"), 0, 100)
	if err != nil || len(reopenedPage.Items) != 2 || reopenedPage.Maintenance.Collected != 1 {
		t.Fatalf("维护审计重启恢复失败: page=%+v err=%v", reopenedPage, err)
	}
}

func TestManagedAuditLifecycleIsTenantIsolatedAndPaged(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	service, err := openTestServiceWithOptions(filepath.Join(t.TempDir(), "credentials.json"), &fakeTransit{}, ServiceOptions{
		Maintenance: MaintenancePolicy{PreparingMaxAge: time.Hour, AbortedRetention: 2 * time.Hour, AuditRetention: 4 * time.Hour, Interval: time.Hour, BatchSize: 20},
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	owner := managedContext("tenant-a", "plugin.database")
	staged, err := service.StageManaged(context.Background(), owner, "database.connection", "primary", []byte("never-in-audit"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ActivateManaged(owner, staged.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RetireManaged(owner, staged.Ref.Handle); err != nil {
		t.Fatal(err)
	}
	first, err := service.ListManagedAudit(auditContext("tenant-a"), 0, 2)
	if err != nil || len(first.Items) != 2 || first.NextBeforeID == 0 || first.Items[0].Action != "managed.retired" {
		t.Fatalf("审计首页错误: page=%+v err=%v", first, err)
	}
	second, err := service.ListManagedAudit(auditContext("tenant-a"), first.NextBeforeID, 2)
	if err != nil || len(second.Items) != 1 || second.Items[0].Action != "managed.staged" {
		t.Fatalf("审计翻页错误: page=%+v err=%v", second, err)
	}
	if second.NextBeforeID != 0 {
		t.Fatalf("没有更旧事件时不得返回虚假游标: %+v", second)
	}
	empty, err := service.ListManagedAudit(auditContext("tenant-b"), 0, 100)
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("审计必须按租户隔离: page=%+v err=%v", empty, err)
	}
	emptyRaw, _ := json.Marshal(empty)
	if strings.Contains(string(emptyRaw), "lastRunAt") {
		t.Fatalf("尚未执行维护的租户不得返回零时间: %s", emptyRaw)
	}
	if _, err := service.ListManagedAudit(managedContext("tenant-a", "plugin.database"), 0, 100); err == nil {
		t.Fatal("普通插件不得读取管理员审计")
	}
	raw, _ := json.Marshal(first)
	if strings.Contains(string(raw), "never-in-audit") || strings.Contains(string(raw), staged.Ref.Handle) || strings.Contains(string(raw), staged.ID) {
		t.Fatalf("生命周期审计泄露敏感标识: %s", raw)
	}
}

func TestManagedAuditIsBoundedAtWriteTime(t *testing.T) {
	service, err := openTestService(filepath.Join(t.TempDir(), "credentials.json"), &fakeTransit{})
	if err != nil {
		t.Fatal(err)
	}
	record := ManagedRecord{
		Ref:      pluginconfig.ManagedCredentialRef{Handle: "credential://managed/bounded", Scope: "tenant", Owner: "plugin.database", Purpose: "database.connection", Version: 1},
		Resource: "primary", State: managedPreparing,
	}
	for index := 0; index < maximumManagedAuditEvents+1; index++ {
		service.appendManagedAuditLocked("tenant-a", "managed.staged", record, time.Unix(int64(index+1), 0).UTC())
	}
	state := service.data.ManagedAudit["tenant-a"]
	if len(state.Events) != maximumManagedAuditEvents || state.Events[0].ID != 2 || state.Events[len(state.Events)-1].ID != maximumManagedAuditEvents+1 {
		t.Fatalf("审计写入边界失效: next=%d first=%d last=%d len=%d", state.NextID, state.Events[0].ID, state.Events[len(state.Events)-1].ID, len(state.Events))
	}
}
