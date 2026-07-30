package versionworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func testManager(t *testing.T, now *time.Time, maxSessions int) *Manager {
	t.Helper()
	catalog := NewCatalog()
	if err := catalog.RegisterAdapter(NewJSONAdapter()); err != nil {
		t.Fatal(err)
	}
	profile := resourcev1.EnvironmentProfile{
		Protocol: resourcev1.Protocol, ID: "platform-development", Revision: 1,
		Bindings: []resourcev1.ResourceBinding{{ResourceType: "portal.configuration", Namespace: "portal.configuration", Adapter: JSONAdapterID, AllowedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot, ProjectionPolicy: resourcev1.ProjectionDomainHot}},
		Limits:   resourcev1.WorkspaceLimits{MaxSessionsPerTenant: maxSessions, MaxLeaseSeconds: 3600, MaxSnapshotBytes: 1 << 20, MaxOverlayBytes: 1 << 20},
	}
	if err := catalog.RegisterEnvironment(profile); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	manager, err := NewManager(catalog, ManagerOptions{Now: func() time.Time { return *now }, NewSessionID: func() (string, error) {
		sequence++
		return fmt.Sprintf("ws_%016d", sequence), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func openRequest(target string) workspacev1.OpenRequest {
	return workspacev1.OpenRequest{EnvironmentID: "platform-development", Resource: resourcev1.ResourceKey{Type: "portal.configuration", ID: "portal-main"}, TargetHead: target, LeaseSeconds: 60}
}

func writeJSON(t *testing.T, manager *Manager, scope Scope, session workspacev1.Session, raw string) workspacev1.Session {
	t.Helper()
	updated, err := manager.WriteSnapshot(context.Background(), scope, workspacev1.WriteSnapshotRequest{
		SessionID: session.ID, ExpectedRevision: session.Revision,
		Snapshot: resourcev1.Snapshot{Kind: resourcev1.ContentJSON, MediaType: "application/json", JSON: json.RawMessage(raw)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func TestManagerSnapshotLifecycleAndIdempotentCommit(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	manager, ledger := testManager(t, &now, 4), newMemoryLedger()
	scope := Scope{TenantID: "tenant-a", ActorID: "plugin:portal"}
	session, err := manager.Open(context.Background(), scope, ledger, openRequest("draft"))
	if err != nil {
		t.Fatal(err)
	}
	session = writeJSON(t, manager, scope, session, `{"name":"main","enabled":true}`)
	changes, err := manager.Changes(context.Background(), scope, workspacev1.SessionRequest{SessionID: session.ID})
	if err != nil || !changes.Dirty || changes.Summary.Added != 2 {
		t.Fatalf("changes 无效: %+v err=%v", changes, err)
	}
	request := workspacev1.CommitRequest{SessionID: session.ID, ExpectedRevision: session.Revision, Message: "create portal"}
	committed, err := manager.Commit(context.Background(), scope, ledger, request)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Session.State != workspacev1.StateCommitted || committed.Head == nil || committed.Head.Target != committed.Version.Ref || len(ledger.versions) != 1 {
		t.Fatalf("commit 无效: %+v", committed)
	}
	retried, err := manager.Commit(context.Background(), scope, ledger, request)
	if err != nil || retried.Version.Ref != committed.Version.Ref || len(ledger.versions) != 1 {
		t.Fatalf("commit 重试不幂等: %+v err=%v", retried, err)
	}
	raw, _ := json.Marshal(committed)
	if _, err := workspacev1.ParseResult(workspacev1.OperationCommit, raw); err != nil {
		t.Fatalf("commit 结果不符合 Wire 契约: %v", err)
	}
}

func TestManagerLeaseQuotaOwnershipAndReadOnly(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	manager, ledger := testManager(t, &now, 1), newMemoryLedger()
	owner := Scope{TenantID: "tenant-a", ActorID: "plugin:portal"}
	session, err := manager.Open(context.Background(), owner, ledger, openRequest(""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), Scope{TenantID: "tenant-a", ActorID: "plugin:other"}, ledger, openRequest("")); errorCode(err) != workspacev1.ErrorLimitExceeded {
		t.Fatalf("租户配额未生效: %v", err)
	}
	if _, err := manager.Status(Scope{TenantID: "tenant-a", ActorID: "plugin:other"}, workspacev1.SessionRequest{SessionID: session.ID}); errorCode(err) != workspacev1.ErrorSessionNotFound {
		t.Fatalf("跨 actor Session 必须隐藏: %v", err)
	}
	now = now.Add(61 * time.Second)
	status, err := manager.Status(owner, workspacev1.SessionRequest{SessionID: session.ID})
	if err != nil || status.State != workspacev1.StateExpired {
		t.Fatalf("Lease 未过期: %+v err=%v", status, err)
	}
	readOnly := openRequest("")
	readOnly.ReadOnly = true
	opened, err := manager.Open(context.Background(), owner, ledger, readOnly)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.WriteSnapshot(context.Background(), owner, workspacev1.WriteSnapshotRequest{SessionID: opened.ID, ExpectedRevision: opened.Revision, Snapshot: resourcev1.Snapshot{Kind: resourcev1.ContentJSON, MediaType: "application/json", JSON: json.RawMessage(`{}`)}}); errorCode(err) != workspacev1.ErrorReadOnly {
		t.Fatalf("只读 Session 写入未拒绝: %v", err)
	}
}

func TestManagerRestoresTransientCommitAndRecoversLostHeadResponse(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	manager, ledger := testManager(t, &now, 2), newMemoryLedger()
	scope := Scope{TenantID: "tenant-a", ActorID: "plugin:portal"}
	session, err := manager.Open(context.Background(), scope, ledger, openRequest("draft"))
	if err != nil {
		t.Fatal(err)
	}
	session = writeJSON(t, manager, scope, session, `{"name":"main"}`)
	request := workspacev1.CommitRequest{SessionID: session.ID, ExpectedRevision: session.Revision}
	ledger.failPutOnce = true
	if _, err := manager.Commit(context.Background(), scope, ledger, request); errorCode(err) != workspacev1.ErrorLedgerUnavailable {
		t.Fatalf("暂时 Ledger 错误未映射: %v", err)
	}
	status, err := manager.Status(scope, workspacev1.SessionRequest{SessionID: session.ID})
	if err != nil || status.State != workspacev1.StateDirty || status.Revision != request.ExpectedRevision {
		t.Fatalf("失败提交未恢复 Session: %+v err=%v", status, err)
	}
	ledger.loseHeadResponse = true
	committed, err := manager.Commit(context.Background(), scope, ledger, request)
	if err != nil || committed.Head == nil || committed.Head.Target != committed.Version.Ref {
		t.Fatalf("Head 响应丢失未恢复: %+v err=%v", committed, err)
	}
}

func TestManagerRejectsStaleHeadWithoutLosingSession(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	manager, ledger := testManager(t, &now, 8), newMemoryLedger()
	scope := Scope{TenantID: "tenant-a", ActorID: "plugin:portal"}
	root, err := manager.Open(context.Background(), scope, ledger, openRequest("draft"))
	if err != nil {
		t.Fatal(err)
	}
	root = writeJSON(t, manager, scope, root, `{"revision":1}`)
	if _, err := manager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{SessionID: root.ID, ExpectedRevision: root.Revision}); err != nil {
		t.Fatal(err)
	}
	left, err := manager.Open(context.Background(), scope, ledger, openRequest("draft"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := manager.Open(context.Background(), scope, ledger, openRequest("draft"))
	if err != nil {
		t.Fatal(err)
	}
	left = writeJSON(t, manager, scope, left, `{"revision":2,"editor":"left"}`)
	right = writeJSON(t, manager, scope, right, `{"revision":2,"editor":"right"}`)
	if _, err := manager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{SessionID: left.ID, ExpectedRevision: left.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{SessionID: right.ID, ExpectedRevision: right.Revision}); errorCode(err) != workspacev1.ErrorBaseConflict {
		t.Fatalf("陈旧 Head 必须产生 base_conflict: %v", err)
	}
	status, err := manager.Status(scope, workspacev1.SessionRequest{SessionID: right.ID})
	if err != nil || status.State != workspacev1.StateDirty || status.Revision != right.Revision {
		t.Fatalf("Head 冲突后 Session 应保留候选: %+v err=%v", status, err)
	}
}

func TestManagerRevisionCASRenewAndDiscard(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	manager, ledger := testManager(t, &now, 2), newMemoryLedger()
	scope := Scope{TenantID: "tenant-a", ActorID: "plugin:portal"}
	session, err := manager.Open(context.Background(), scope, ledger, openRequest(""))
	if err != nil {
		t.Fatal(err)
	}
	updated := writeJSON(t, manager, scope, session, `{"revision":1}`)
	if _, err := manager.WriteSnapshot(context.Background(), scope, workspacev1.WriteSnapshotRequest{SessionID: session.ID, ExpectedRevision: session.Revision, Snapshot: resourcev1.Snapshot{Kind: resourcev1.ContentJSON, MediaType: "application/json", JSON: json.RawMessage(`{"revision":2}`)}}); errorCode(err) != workspacev1.ErrorSessionConflict {
		t.Fatalf("陈旧 revision 写入未拒绝: %v", err)
	}
	renewed, err := manager.Renew(scope, workspacev1.RenewRequest{SessionID: updated.ID, ExpectedRevision: updated.Revision, LeaseSeconds: 120})
	if err != nil || !renewed.LeaseExpiresAt.Equal(now.Add(120*time.Second)) {
		t.Fatalf("续租失败: %+v err=%v", renewed, err)
	}
	discarded, err := manager.Discard(scope, workspacev1.RevisionRequest{SessionID: renewed.ID, ExpectedRevision: renewed.Revision})
	if err != nil || discarded.State != workspacev1.StateDiscarded {
		t.Fatalf("丢弃失败: %+v err=%v", discarded, err)
	}
	if _, err := manager.ReadSnapshot(scope, workspacev1.SessionRequest{SessionID: discarded.ID}); errorCode(err) != workspacev1.ErrorSessionConflict {
		t.Fatalf("丢弃后不应继续读取候选: %v", err)
	}
}

func errorCode(err error) string {
	var workspaceErr *WorkspaceError
	if errors.As(err, &workspaceErr) {
		return workspaceErr.Code
	}
	return ""
}
