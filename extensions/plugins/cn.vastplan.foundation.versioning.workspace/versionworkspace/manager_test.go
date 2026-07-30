package versionworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	request := workspacev1.CommitRequest{SessionID: session.ID, ExpectedRevision: session.Revision, OperationID: "portal-publication:0001", Message: "create portal"}
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

func TestManagerOperationIdentitySurvivesSessionLossAndCommittedReads(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	ledger := newMemoryLedger()
	scope := Scope{TenantID: "tenant-a", ActorID: "plugin:portal"}
	firstManager := testManager(t, &now, 4)
	firstSession, err := firstManager.Open(context.Background(), scope, ledger, openRequest(""))
	if err != nil {
		t.Fatal(err)
	}
	firstSession = writeJSON(t, firstManager, scope, firstSession, `{"name":"main","revision":1}`)
	operationID := "portal-publication:stable-retry"
	first, err := firstManager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{
		SessionID: firstSession.ID, ExpectedRevision: firstSession.Revision, OperationID: operationID, Message: "submit portal",
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := firstManager.ReadCommitted(context.Background(), scope, ledger, workspacev1.CommittedRequest{
		EnvironmentID: "platform-development", EnvironmentDigest: first.Session.EnvironmentDigest, Resource: firstSession.Resource, Ref: first.Version.Ref,
	})
	if err != nil || read.Version.Ref != first.Version.Ref || string(read.Snapshot.JSON) != `{"name":"main","revision":1}` {
		t.Fatalf("读取已提交版本失败: %+v err=%v", read, err)
	}

	// 模拟 Workspace Leader 重启：新 Manager 不含原 Session，先消耗一个 ID，
	// 确保重试发生在不同 Session 中。
	secondManager := testManager(t, &now, 4)
	dummy := openRequest("")
	dummy.Resource.ID = "dummy"
	dummy.ReadOnly = true
	if _, err := secondManager.Open(context.Background(), scope, ledger, dummy); err != nil {
		t.Fatal(err)
	}
	reopened, err := secondManager.Open(context.Background(), scope, ledger, openRequest(""))
	if err != nil || reopened.ID == firstSession.ID {
		t.Fatalf("未建立不同的重试 Session: %+v err=%v", reopened, err)
	}
	reopened = writeJSON(t, secondManager, scope, reopened, `{"name":"main","revision":1}`)
	retried, err := secondManager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{
		SessionID: reopened.ID, ExpectedRevision: reopened.Revision, OperationID: operationID, Message: "submit portal",
	})
	if err != nil || retried.Version.Ref != first.Version.Ref || len(ledger.versions) != 1 {
		t.Fatalf("跨 Session operationId 重试未复用版本: %+v err=%v versions=%d", retried, err, len(ledger.versions))
	}

	nextRequest := openRequest("")
	nextRequest.BaseRef = &first.Version.Ref
	next, err := secondManager.Open(context.Background(), scope, ledger, nextRequest)
	if err != nil {
		t.Fatal(err)
	}
	next = writeJSON(t, secondManager, scope, next, `{"name":"main","revision":2}`)
	second, err := secondManager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{
		SessionID: next.ID, ExpectedRevision: next.Revision, OperationID: "portal-publication:next", Message: "submit portal v2",
	})
	if err != nil {
		t.Fatal(err)
	}
	compared, err := secondManager.CompareCommitted(context.Background(), scope, ledger, workspacev1.CompareCommittedRequest{
		EnvironmentID: "platform-development", EnvironmentDigest: second.Session.EnvironmentDigest, Resource: next.Resource, Left: first.Version.Ref, Right: second.Version.Ref,
	})
	if err != nil || !compared.Dirty || !compared.DiffAvailable || len(compared.ChangedPaths) != 1 || compared.ChangedPaths[0] != "/revision" {
		t.Fatalf("比较已提交版本失败: %+v err=%v", compared, err)
	}
	for operation, value := range map[string]any{workspacev1.OperationReadCommitted: read, workspacev1.OperationCompareCommitted: compared} {
		raw, _ := json.Marshal(value)
		if _, err := workspacev1.ParseResult(operation, raw); err != nil {
			t.Fatalf("%s 结果不符合 Wire 契约: %v", operation, err)
		}
	}
	missing := second.Version.Ref
	missing.VersionID = strings.Repeat("f", 64)
	if _, err := secondManager.ReadCommitted(context.Background(), scope, ledger, workspacev1.CommittedRequest{EnvironmentID: "platform-development", EnvironmentDigest: second.Session.EnvironmentDigest, Resource: next.Resource, Ref: missing}); errorCode(err) != workspacev1.ErrorVersionNotFound {
		t.Fatalf("缺失 VersionRef 错误未稳定映射: %v", err)
	}
	if _, err := secondManager.ReadCommitted(context.Background(), scope, ledger, workspacev1.CommittedRequest{EnvironmentID: "platform-development", EnvironmentDigest: strings.Repeat("0", 64), Resource: next.Resource, Ref: second.Version.Ref}); errorCode(err) != workspacev1.ErrorEnvironmentNotFound {
		t.Fatalf("缺失 Environment digest 必须失败关闭: %v", err)
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
	request := workspacev1.CommitRequest{SessionID: session.ID, ExpectedRevision: session.Revision, OperationID: "portal-publication:0002"}
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
	if _, err := manager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{SessionID: root.ID, ExpectedRevision: root.Revision, OperationID: "portal-publication:root"}); err != nil {
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
	if _, err := manager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{SessionID: left.ID, ExpectedRevision: left.Revision, OperationID: "portal-publication:left"}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Commit(context.Background(), scope, ledger, workspacev1.CommitRequest{SessionID: right.ID, ExpectedRevision: right.Revision, OperationID: "portal-publication:right"}); errorCode(err) != workspacev1.ErrorBaseConflict {
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
