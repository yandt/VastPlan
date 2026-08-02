package portalcomposer

import (
	"context"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func TestPluginInstallationPreparationKeepsWorkingCopyAndSupportsRollback(t *testing.T) {
	service := newTestService(t)
	author := principal("author")
	approver := principal("approver")
	publisher := principal("publisher")
	configuration, err := service.configurationFromCatalog(spec("/"), author.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	portal, err := service.CreatePortal(context.Background(), author, portalapi.CreatePortalRequest{PortalID: "operations", Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	publication, err := service.SubmitPortalPublication(context.Background(), author, portal.ID, portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: portal.WorkingCopy.Revision})
	if err != nil {
		t.Fatal(err)
	}
	publication, err = service.ApprovePortalPublication(withDifferentSubjectTestPolicy(context.Background()), approver, portal.ID, publication.ID, portalapi.PortalApprovalRequest{})
	if err != nil {
		t.Fatal(err)
	}
	publication, err = service.PublishPortalPublication(context.Background(), publisher, portal.ID, publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	release, err := service.ReleasePortalPublication(context.Background(), publisher, portal.ID, portalapi.PortalPublicationReleaseRequest{PublicationID: publication.ID})
	if err != nil {
		t.Fatal(err)
	}
	workingConfiguration := configuration
	workingConfiguration.Application.Route = "/next"
	if _, err := service.CreatePortalWorkingCopy(context.Background(), author, portal.ID, workingConfiguration); err != nil {
		t.Fatal(err)
	}

	system := portalapi.Principal{ID: "system", TenantID: author.TenantID, System: true}
	artifact := pluginv1.ArtifactRef{PluginID: "cn.vastplan.example.feature", Version: "1.0.0", Channel: "stable"}
	request := portalapi.PluginInstallationRequest{
		CandidateID: "installation-0123456789abcdef", PortalID: portal.ID,
		Action: plugininstallation.ActionInstall, PluginID: artifact.PluginID, Artifact: &artifact,
	}
	prepared, err := service.PreparePluginInstallation(context.Background(), system, request)
	if err != nil || prepared.Status != portalapi.PluginInstallationPrepared || prepared.PreviousActivationID != release.ID {
		t.Fatalf("Portal 安装预热失败: %+v err=%v", prepared, err)
	}
	governance, err := service.PortalGovernance(context.Background(), author)
	if err != nil || governance.Portals[0].WorkingCopy == nil || governance.Portals[0].WorkingCopy.Configuration.Application.Route != "/next" {
		t.Fatalf("安装候选不得覆盖用户 WorkingCopy: %+v err=%v", governance, err)
	}
	committed, err := service.CommitPluginInstallation(context.Background(), system, portalapi.PluginInstallationLookup{CandidateID: request.CandidateID, PortalID: portal.ID})
	if err != nil || committed.Status != portalapi.PluginInstallationCommitted || committed.ActivationID == 0 {
		t.Fatalf("Portal 安装提交失败: %+v err=%v", committed, err)
	}
	governance, err = service.PortalGovernance(context.Background(), author)
	current := governance.Portals[0].Releases[0]
	if err != nil || current.ID != committed.ActivationID || !portalSpecContains(current.Resolved, artifact.PluginID, artifact.Version) {
		t.Fatalf("Portal 当前代未切换到目标插件: %+v err=%v", current, err)
	}
	rolledBack, err := service.RollbackPluginInstallation(context.Background(), system, portalapi.PluginInstallationLookup{CandidateID: request.CandidateID, PortalID: portal.ID})
	if err != nil || rolledBack.Status != portalapi.PluginInstallationRolledBack {
		t.Fatalf("Portal 安装回滚失败: %+v err=%v", rolledBack, err)
	}
}

func portalSpecContains(spec portalapi.PortalSpec, pluginID, version string) bool {
	for _, plugin := range spec.Plugins {
		if plugin.ID == pluginID && plugin.Version == version {
			return true
		}
	}
	return false
}
