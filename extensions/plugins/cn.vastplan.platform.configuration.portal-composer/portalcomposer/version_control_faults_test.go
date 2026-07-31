package portalcomposer

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

var testPortalVersionControlBinding = PortalVersionControlBinding{
	EnvironmentID: "platform-production",
	ResourceType:  "portal.configuration",
}

func TestPortalVersionControlProjectsOnlySupportedCapabilities(t *testing.T) {
	service := newTestService(t)
	if err := service.BindVersionControl(&testPortalVersionControlBinding); err != nil {
		t.Fatal(err)
	}
	control := newFakePortalVersionControl()
	control.capabilities.Diff = false
	ctx := withVersionControl(context.Background(), control)
	author := principal("author")
	portal, _ := createPortalAggregateForTest(t, service, ctx, author)

	assertPortalVersionCapabilities(t, service, ctx, author, portal.ID, []string{"history", "read", "restore"})
	control.capabilities = PortalVersionControlCapabilities{}
	assertPortalVersionCapabilities(t, service, ctx, author, portal.ID, []string{"history"})
	control.capabilities = PortalVersionControlCapabilities{Diff: true, Restore: true}
	assertPortalVersionCapabilities(t, service, ctx, author, portal.ID, []string{"history", "diff"})
}

func TestPortalVersionControlMissingSoftDependencyFailsOnlyBoundPortal(t *testing.T) {
	service := newTestService(t)
	if err := service.BindVersionControl(&testPortalVersionControlBinding); err != nil {
		t.Fatal(err)
	}
	author := principal("author")
	portal, _ := createPortalAggregateForTest(t, service, context.Background(), author)

	governance, err := service.PortalGovernance(context.Background(), author)
	if err != nil || len(governance.Portals) != 1 || governance.Portals[0].VersionControl.Availability != portalapi.PortalVersionControlUnavailable || governance.Portals[0].WorkingCopy == nil {
		t.Fatalf("缺少软依赖时必须保留热投影并明确标记不可用: %+v err=%v", governance, err)
	}
	request := portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: portal.WorkingCopy.Revision}
	if _, err := service.SubmitPortalPublication(context.Background(), author, portal.ID, request); !errors.Is(err, ErrVersionControlUnavailable) {
		t.Fatalf("已绑定 Portal 不得静默降级为 inline Publication: %v", err)
	}

	unversioned := newTestService(t)
	plainPortal, configuration := createPortalAggregateForTest(t, unversioned, context.Background(), author)
	publication, err := unversioned.SubmitPortalPublication(context.Background(), author, plainPortal.ID, portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: plainPortal.WorkingCopy.Revision})
	if err != nil || publication.Source.Kind != portalapi.PortalPublicationSourceInline || publication.Source.Configuration == nil || publication.Source.Configuration.Application.Route != configuration.Application.Route {
		t.Fatalf("未绑定 Portal 不得因 Workspace 缺失而降级: %+v err=%v", publication, err)
	}
}

func TestPortalVersionSubmitRecoversAfterLeaderRestartAndLostResponse(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "portals.json")
	service := openVersionedTestService(t, stateFile)
	control := newFakePortalVersionControl()
	control.failAfterCommit = true
	ctx := withVersionControl(context.Background(), control)
	author := principal("author")
	portal, _ := createPortalAggregateForTest(t, service, ctx, author)
	request := portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: portal.WorkingCopy.Revision}

	if _, err := service.SubmitPortalPublication(ctx, author, portal.ID, request); !errors.Is(err, ErrVersionControlUnavailable) {
		t.Fatalf("模拟 Workspace 已提交但响应丢失失败: %v", err)
	}
	if len(control.results) != 1 || len(control.calls) != 1 {
		t.Fatalf("响应丢失前应只产生一个外部版本: results=%d calls=%d", len(control.results), len(control.calls))
	}

	restarted := openVersionedTestService(t, stateFile)
	publication, err := restarted.SubmitPortalPublication(ctx, author, portal.ID, request)
	if err != nil || publication.Source.VersionRef == nil {
		t.Fatalf("Leader 重启后未使用持久 operationId 恢复: %+v err=%v", publication, err)
	}
	if len(control.calls) != 2 || control.calls[0].OperationID != control.calls[1].OperationID || len(control.results) != 1 {
		t.Fatalf("跨重启重试必须复用同一逻辑版本: calls=%+v results=%d", control.calls, len(control.results))
	}
	if retried, retryErr := restarted.SubmitPortalPublication(ctx, author, portal.ID, request); retryErr != nil || retried.ID != publication.ID || len(control.calls) != 2 {
		t.Fatalf("聚合提交成功但响应丢失时重试应直接返回同一 Publication: %+v err=%v calls=%d", retried, retryErr, len(control.calls))
	}
	history, err := restarted.PortalVersionHistory(context.Background(), author, portal.ID)
	if err != nil || len(history.Entries) != 1 || history.Entries[0].VersionRef.VersionID != publication.Source.VersionRef.VersionID {
		t.Fatalf("恢复后聚合只能确认一个历史版本: %+v err=%v", history, err)
	}
}

func TestPortalVersionAggregateConflictKeepsExternalCommitUnreachableUntilRetry(t *testing.T) {
	service := newTestService(t)
	if err := service.BindVersionControl(&testPortalVersionControlBinding); err != nil {
		t.Fatal(err)
	}
	control := newFakePortalVersionControl()
	ctx := withVersionControl(context.Background(), control)
	author := principal("author")
	portal, _ := createPortalAggregateForTest(t, service, ctx, author)
	var hookErr error
	control.afterCommit = func(PortalVersionCommitRequest) {
		service.mu.Lock()
		defer service.mu.Unlock()
		state := service.state.VersionControls[portal.ID]
		state.Pending = nil
		service.state.VersionControls[portal.ID] = state
		hookErr = service.save()
	}
	request := portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: portal.WorkingCopy.Revision}

	if _, err := service.SubmitPortalPublication(ctx, author, portal.ID, request); !errors.Is(err, ErrStateConflict) {
		t.Fatalf("外部提交后的聚合冲突必须失败关闭: %v", err)
	}
	if hookErr != nil {
		t.Fatal(hookErr)
	}
	history, err := service.PortalVersionHistory(context.Background(), author, portal.ID)
	if err != nil || len(history.Entries) != 0 || len(control.results) != 1 {
		t.Fatalf("聚合未确认的外部版本不得进入可见历史: history=%+v results=%d err=%v", history, len(control.results), err)
	}
	publication, err := service.SubmitPortalPublication(ctx, author, portal.ID, request)
	if err != nil || publication.Source.VersionRef == nil || len(control.results) != 1 || len(control.calls) != 2 || control.calls[0].OperationID != control.calls[1].OperationID {
		t.Fatalf("冲突重试必须幂等确认原外部版本: publication=%+v calls=%+v results=%d err=%v", publication, control.calls, len(control.results), err)
	}
}

func TestPortalReleaseAndHotProjectionSurviveColdVersionControlFailure(t *testing.T) {
	service := newTestService(t)
	if err := service.BindVersionControl(&testPortalVersionControlBinding); err != nil {
		t.Fatal(err)
	}
	control := newFakePortalVersionControl()
	ctx := withVersionControl(context.Background(), control)
	author, approver, publisher := principal("author"), principal("approver"), principal("publisher")
	portal, _ := createPortalAggregateForTest(t, service, ctx, author)
	publication, err := service.SubmitPortalPublication(ctx, author, portal.ID, portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: portal.WorkingCopy.Revision})
	if err == nil {
		publication, err = service.ApprovePortalPublication(ctx, approver, portal.ID, publication.ID)
	}
	if err == nil {
		publication, err = service.PublishPortalPublication(ctx, publisher, portal.ID, publication.ID)
	}
	if err != nil {
		t.Fatal(err)
	}

	control.describeErr = ErrVersionControlUnavailable
	control.readErr = ErrVersionControlUnavailable
	release, err := service.ReleasePortalPublication(context.Background(), publisher, portal.ID, portalapi.PortalPublicationReleaseRequest{PublicationID: publication.ID})
	if err != nil || release.Status != portalapi.ActivationCurrent {
		t.Fatalf("Release 不得依赖 Workspace/Ledger 在线: %+v err=%v", release, err)
	}
	governance, err := service.PortalGovernance(ctx, author)
	if err != nil || len(governance.Portals) != 1 || governance.Portals[0].VersionControl.Availability != portalapi.PortalVersionControlUnavailable || governance.Portals[0].PublishedPublication == nil || governance.Portals[0].CurrentReleaseID != release.ID {
		t.Fatalf("冷历史故障不得破坏当前 Publication/Release 热投影: %+v err=%v", governance, err)
	}
	history, err := service.PortalVersionHistory(context.Background(), author, portal.ID)
	if err != nil || len(history.Entries) != 1 {
		t.Fatalf("聚合已确认的轻量历史必须保持可列出: %+v err=%v", history, err)
	}
	if _, err := service.ReadPortalVersion(ctx, author, portal.ID, history.Entries[0].VersionRef.VersionID); !errors.Is(err, ErrVersionControlUnavailable) {
		t.Fatalf("冷历史读取故障必须保持稳定错误: %v", err)
	}
	releases, err := service.ListPortalReleases(context.Background(), publisher)
	if err != nil || len(releases) != 1 || releases[0].ID != release.ID {
		t.Fatalf("运行时 Release 读取不得 hydrate Ledger: %+v err=%v", releases, err)
	}
}

func createPortalAggregateForTest(t *testing.T, service *Service, ctx context.Context, author portalapi.Principal) (portalapi.Portal, portalapi.PortalConfiguration) {
	t.Helper()
	configuration, err := service.configurationFromCatalog(spec("/"), author.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	portal, err := service.CreatePortal(ctx, author, portalapi.CreatePortalRequest{PortalID: "admin", Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	return portal, configuration
}

func openVersionedTestService(t *testing.T, path string) *Service {
	t.Helper()
	service, err := openTestService(path, acceptingCatalog{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	}
	if err := service.BindVersionControl(&testPortalVersionControlBinding); err != nil {
		t.Fatal(err)
	}
	return service
}

func assertPortalVersionCapabilities(t *testing.T, service *Service, ctx context.Context, principal portalapi.Principal, portalID string, expected []string) {
	t.Helper()
	governance, err := service.PortalGovernance(ctx, principal)
	if err != nil || len(governance.Portals) != 1 || governance.Portals[0].ID != portalID {
		t.Fatalf("读取 Portal 能力失败: %+v err=%v", governance, err)
	}
	status := governance.Portals[0].VersionControl
	if status.Availability != portalapi.PortalVersionControlAvailable || !slices.Equal(status.Capabilities, expected) {
		t.Fatalf("版本能力投影错误: got=%+v want=%v", status, expected)
	}
}
