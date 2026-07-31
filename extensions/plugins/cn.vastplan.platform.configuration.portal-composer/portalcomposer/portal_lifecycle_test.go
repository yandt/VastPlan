package portalcomposer

import (
	"context"
	"errors"
	"testing"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func TestPortalNoVersionLifecycleUsesWorkingCopyPublicationAndRelease(t *testing.T) {
	service := newTestService(t)
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	configuration, err := service.configurationFromCatalog(spec("/"), author.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	portal, err := service.CreatePortal(context.Background(), author, portalapi.CreatePortalRequest{PortalID: "admin", Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	if portal.WorkingCopy == nil || portal.WorkingCopy.Revision != 1 || portal.PendingPublication != nil || portal.PublishedPublication != nil {
		t.Fatalf("Portal 初始 WorkingCopy 投影错误: %+v", portal)
	}
	if portal.VersionControl.Enabled || portal.VersionControl.Availability != portalapi.PortalVersionControlDisabled || len(portal.VersionControl.Capabilities) != 0 {
		t.Fatalf("未配置版本控制时不得伪造版本能力: %+v", portal.VersionControl)
	}
	configuration.Application.Route = "/updated"
	if _, err := service.SavePortalWorkingCopy(context.Background(), author, portal.ID, portalapi.SavePortalWorkingCopyRequest{ExpectedRevision: 2, Configuration: configuration}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("陈旧或超前 WorkingCopy revision 必须拒绝: %v", err)
	}
	working, err := service.SavePortalWorkingCopy(context.Background(), author, portal.ID, portalapi.SavePortalWorkingCopyRequest{ExpectedRevision: 1, Configuration: configuration})
	if err != nil || working.Revision != 2 || working.Configuration.Application.Route != "/updated" || len(working.Digest) != 64 {
		t.Fatalf("保存 WorkingCopy 失败: %+v err=%v", working, err)
	}
	publication, err := service.SubmitPortalPublication(context.Background(), author, portal.ID, portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: working.Revision})
	if err != nil || publication.Status != portalapi.StatusPendingApproval || publication.WorkingRevision != working.Revision || publication.Source.Kind != portalapi.PortalPublicationSourceInline || publication.Source.Configuration == nil {
		t.Fatalf("冻结 Publication 失败: %+v err=%v", publication, err)
	}
	publication.Source.Configuration.Application.Route = "/tampered-client-copy"
	governance, err := service.PortalGovernance(context.Background(), author)
	if err != nil || len(governance.Portals) != 1 || governance.Portals[0].WorkingCopy != nil || governance.Portals[0].PendingPublication == nil || governance.Portals[0].PendingPublication.Source.Configuration.Application.Route != "/updated" {
		t.Fatalf("Publication 必须是隔离冻结快照: %+v err=%v", governance, err)
	}
	if _, err := service.SavePortalWorkingCopy(context.Background(), author, portal.ID, portalapi.SavePortalWorkingCopyRequest{ExpectedRevision: working.Revision, Configuration: configuration}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("候选审批期间不得继续修改 WorkingCopy: %v", err)
	}
	if _, err := service.ApprovePortalPublication(context.Background(), author, portal.ID, publication.ID); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("Publication 必须异人审批: %v", err)
	}
	publication, err = service.ApprovePortalPublication(context.Background(), approver, portal.ID, publication.ID)
	if err != nil || publication.Status != portalapi.StatusApproved {
		t.Fatalf("审批 Publication 失败: %+v err=%v", publication, err)
	}
	publication, err = service.PublishPortalPublication(context.Background(), publisher, portal.ID, publication.ID)
	if err != nil || publication.Status != portalapi.StatusPublished {
		t.Fatalf("发布 Publication 失败: %+v err=%v", publication, err)
	}
	release, err := service.ReleasePortalPublication(context.Background(), publisher, portal.ID, portalapi.PortalPublicationReleaseRequest{PublicationID: publication.ID})
	if err != nil || release.Status != portalapi.ActivationCurrent || release.PublicationID != publication.ID {
		t.Fatalf("上线 Publication 失败: %+v err=%v", release, err)
	}
	next := configuration
	next.Application.Route = "/next"
	nextWorking, err := service.CreatePortalWorkingCopy(context.Background(), author, portal.ID, next)
	if err != nil || nextWorking.Revision != 1 {
		t.Fatalf("Published 后创建下一轮 WorkingCopy 失败: %+v err=%v", nextWorking, err)
	}
	audit, err := service.Audit(context.Background(), author, portal.ID, publication.ID)
	if err != nil || len(audit) != 5 || audit[1].Action != "portal.working-copy.saved" || audit[2].Action != "portal.publication.submit" {
		t.Fatalf("新生命周期审计不完整: %+v err=%v", audit, err)
	}
}
