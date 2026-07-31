package portalcomposer

import (
	"context"
	"fmt"
	"testing"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

type fakePortalVersionControl struct {
	failNext bool
	calls    []PortalVersionCommitRequest
	results  map[string]PortalVersionCommitResult
	content  map[string]portalapi.PortalConfiguration
}

func newFakePortalVersionControl() *fakePortalVersionControl {
	return &fakePortalVersionControl{
		results: map[string]PortalVersionCommitResult{},
		content: map[string]portalapi.PortalConfiguration{},
	}
}

func (*fakePortalVersionControl) Describe(context.Context, PortalVersionControlBinding, string) (PortalVersionControlCapabilities, error) {
	return PortalVersionControlCapabilities{Read: true, Diff: true, Restore: true}, nil
}

func (f *fakePortalVersionControl) Commit(_ context.Context, request PortalVersionCommitRequest) (PortalVersionCommitResult, error) {
	f.calls = append(f.calls, request)
	if f.failNext {
		f.failNext = false
		return PortalVersionCommitResult{}, ErrVersionControlUnavailable
	}
	if result, ok := f.results[request.OperationID]; ok {
		return result, nil
	}
	versionID := fmt.Sprintf("version-%d", len(f.results)+1)
	result := PortalVersionCommitResult{
		EnvironmentDigest: "environment-digest",
		VersionRef: versioningv1.VersionRef{
			Stream:    versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: request.PortalID},
			VersionID: versionID, Sequence: uint64(len(f.results) + 1), ContentDigest: versionID + "-digest",
		},
		Capabilities: PortalVersionControlCapabilities{Read: true, Diff: true, Restore: true},
	}
	f.results[request.OperationID] = result
	f.content[versionID] = request.Configuration
	return result, nil
}

func (f *fakePortalVersionControl) Read(_ context.Context, request PortalVersionReadRequest) (portalapi.PortalConfiguration, error) {
	configuration, ok := f.content[request.VersionRef.VersionID]
	if !ok {
		return portalapi.PortalConfiguration{}, ErrVersionControlUnavailable
	}
	return configuration, nil
}

func (*fakePortalVersionControl) Compare(_ context.Context, request PortalVersionCompareRequest) (PortalVersionCompareResult, error) {
	return PortalVersionCompareResult{
		Dirty: request.Left.VersionID != request.Right.VersionID, DiffAvailable: true,
		ChangedPaths: []string{"/application/route"}, Summary: versionresourcev1.ChangeSummary{Modified: 1},
	}, nil
}

func TestPortalOptionalVersionControlCommitHistoryCompareAndRestore(t *testing.T) {
	service := newTestService(t)
	binding := PortalVersionControlBinding{EnvironmentID: "platform-production", ResourceType: "portal.configuration"}
	if err := service.BindVersionControl(&binding); err != nil {
		t.Fatal(err)
	}
	control := newFakePortalVersionControl()
	ctx := withVersionControl(context.Background(), control)
	author, approver, publisher := principal("author"), principal("approver"), principal("publisher")
	configuration, err := service.configurationFromCatalog(spec("/first"), author.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	portal, err := service.CreatePortal(ctx, author, portalapi.CreatePortalRequest{PortalID: "admin", Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	if !portal.VersionControl.Enabled || portal.VersionControl.Availability != portalapi.PortalVersionControlUnavailable {
		t.Fatalf("首次提交前应声明已启用但尚未确认可用: %+v", portal.VersionControl)
	}
	governance, err := service.PortalGovernance(ctx, author)
	if err != nil || len(governance.Portals) != 1 || governance.Portals[0].VersionControl.Availability != portalapi.PortalVersionControlAvailable {
		t.Fatalf("管理读取应按端口能力投影当前可用性: %+v err=%v", governance, err)
	}

	control.failNext = true
	request := portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: portal.WorkingCopy.Revision}
	if _, err := service.SubmitPortalPublication(ctx, author, portal.ID, request); err != ErrVersionControlUnavailable {
		t.Fatalf("Workspace 故障必须失败关闭且保留热工作副本: %v", err)
	}
	if _, err := service.SavePortalWorkingCopy(ctx, author, portal.ID, portalapi.SavePortalWorkingCopyRequest{ExpectedRevision: portal.WorkingCopy.Revision, Configuration: configuration}); err == nil {
		t.Fatal("待恢复的外部提交必须冻结 WorkingCopy，避免响应丢失时产生歧义")
	}
	publication, err := service.SubmitPortalPublication(ctx, author, portal.ID, request)
	if err != nil || publication.Source.Kind != portalapi.PortalPublicationSourceWorkspace || publication.Source.VersionRef == nil {
		t.Fatalf("版本化 Publication 提交失败: %+v err=%v", publication, err)
	}
	if len(control.calls) != 2 || control.calls[0].OperationID != control.calls[1].OperationID {
		t.Fatalf("重试必须复用持久 operationId: %+v", control.calls)
	}
	publication, err = service.ApprovePortalPublication(ctx, approver, portal.ID, publication.ID)
	if err == nil {
		publication, err = service.PublishPortalPublication(ctx, publisher, portal.ID, publication.ID)
	}
	if err != nil {
		t.Fatal(err)
	}

	secondConfiguration := configuration
	secondConfiguration.Application.Route = "/second"
	secondWorking, err := service.CreatePortalWorkingCopy(ctx, author, portal.ID, secondConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	secondPublication, err := service.SubmitPortalPublication(ctx, author, portal.ID, portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: secondWorking.Revision})
	if err != nil {
		t.Fatal(err)
	}
	history, err := service.PortalVersionHistory(ctx, author, portal.ID)
	if err != nil || len(history.Entries) != 2 || history.Entries[0].VersionRef.VersionID != secondPublication.Source.VersionRef.VersionID {
		t.Fatalf("Portal 聚合历史错误: %+v err=%v", history, err)
	}
	comparison, err := service.ComparePortalVersions(ctx, author, portal.ID, history.Entries[1].VersionRef.VersionID, history.Entries[0].VersionRef.VersionID)
	if err != nil || !comparison.Dirty || !comparison.DiffAvailable || len(comparison.ChangedPaths) != 1 {
		t.Fatalf("历史比较错误: %+v err=%v", comparison, err)
	}
	if _, err := service.ApprovePortalPublication(ctx, approver, portal.ID, secondPublication.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishPortalPublication(ctx, publisher, portal.ID, secondPublication.ID); err != nil {
		t.Fatal(err)
	}
	thirdConfiguration := secondConfiguration
	thirdConfiguration.Application.Route = "/third"
	thirdWorking, err := service.CreatePortalWorkingCopy(ctx, author, portal.ID, thirdConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := service.RestorePortalVersion(ctx, author, portal.ID, portalapi.RestorePortalVersionRequest{
		VersionID: history.Entries[1].VersionRef.VersionID, ExpectedWorkingRevision: thirdWorking.Revision,
	})
	if err != nil || restored.Configuration.Application.Route != "/first" || restored.Revision != thirdWorking.Revision+1 {
		t.Fatalf("恢复历史应覆盖 WorkingCopy 而非修改旧版本: %+v err=%v", restored, err)
	}
}
