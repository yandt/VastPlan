package deploymentmanager

import (
	"context"
	"errors"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

func TestPluginInstallationCandidateUsesExistingApprovalActivationAndRollback(t *testing.T) {
	service, host, requester := publishedIntentService(t)
	requester.Scene = "portal.bff"
	candidate, err := service.CreatePluginInstallationCandidate(context.Background(), host, requester, plugininstallation.SourceController, upgradeInstallationRequest())
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Status != plugininstallation.CandidatePlanned || candidate.ServiceRevisionID != 2 || candidate.PreviousServiceRevisionID != 1 || candidate.RequestedBy != "carol" {
		t.Fatalf("持久候选初始状态错误: %+v", candidate)
	}
	revisions, _ := service.ListServiceRevisions(requester)
	if len(revisions) != 2 || revisions[0].Status != "Draft" {
		t.Fatalf("安装候选必须原子创建既有 ServiceRevision 草稿: %+v", revisions)
	}
	if _, err := service.UpdateIntentDraft(context.Background(), host, requester, candidate.ServiceRevisionID, *revisions[0].Intent); !errors.Is(err, errServiceState) {
		t.Fatalf("通用 Intent 编辑入口不得修改安装候选草稿: %v", err)
	}
	if _, err := service.SubmitServiceDraft(context.Background(), host, requester, candidate.ServiceRevisionID); !errors.Is(err, errServiceState) {
		t.Fatalf("通用服务提交入口不得绕过安装候选权限: %v", err)
	}

	submitted, err := service.SubmitPluginInstallationCandidate(context.Background(), host, requester, candidate.ID)
	if err != nil || submitted.Status != plugininstallation.CandidatePendingApproval || submitted.SubmittedBy != "carol" {
		t.Fatalf("安装候选没有复用提交状态: %+v err=%v", submitted, err)
	}
	approver := userCall("tenant-a", "bob")
	if _, err := service.ApproveServiceRevision(context.Background(), host, approver, candidate.ServiceRevisionID); !errors.Is(err, errServiceState) {
		t.Fatalf("通用服务审批入口不得绕过安装候选权限: %v", err)
	}
	approved, err := service.ApprovePluginInstallationCandidate(context.Background(), host, approver, candidate.ID)
	if err != nil || approved.Status != plugininstallation.CandidateApproved || approved.ApprovedBy != "bob" {
		t.Fatalf("安装候选没有复用异人审批: %+v err=%v", approved, err)
	}
	activator := userCall("tenant-a", "alice")
	if _, err := service.PublishServiceRevision(context.Background(), host, activator, candidate.ServiceRevisionID); !errors.Is(err, errServiceState) {
		t.Fatalf("通用服务发布入口不得绕过安装候选权限: %v", err)
	}
	ready, err := service.ActivatePluginInstallationCandidate(context.Background(), host, activator, candidate.ID)
	if err != nil || ready.Status != plugininstallation.CandidateReady || ready.ActivatedBy != "alice" {
		t.Fatalf("安装候选没有经既有发布链激活: %+v err=%v", ready, err)
	}
	revisions, _ = service.ListServiceRevisions(activator)
	if !revisions[0].Active || revisions[0].Intent.Services[0].RootPlugins[0].Constraint != "=2.0.0" {
		t.Fatalf("激活后的活动 Intent 未使用候选版本: %+v", revisions[0])
	}

	rolledBack, err := service.RollbackPluginInstallationCandidate(context.Background(), host, activator, candidate.ID)
	if err != nil || rolledBack.Status != plugininstallation.CandidateRolledBack || rolledBack.RollbackServiceRevisionID != 3 {
		t.Fatalf("安装候选没有通过单调服务修订回滚: %+v err=%v", rolledBack, err)
	}
	revisions, _ = service.ListServiceRevisions(activator)
	if !revisions[0].Active || revisions[0].ID != 3 || revisions[0].Intent.Services[0].RootPlugins[0].Constraint != "=1.0.0" {
		t.Fatalf("回滚没有恢复上一活动 Intent: %+v", revisions[0])
	}
}

func TestPluginInstallationCandidateCancelIsAtomicAndIdempotent(t *testing.T) {
	service, host, requester := publishedIntentService(t)
	requester.Scene = "portal.bff"
	candidate, err := service.CreatePluginInstallationCandidate(context.Background(), host, requester, plugininstallation.SourceController, upgradeInstallationRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePluginInstallationCandidate(context.Background(), host, requester, plugininstallation.SourceController, upgradeInstallationRequest()); !errors.Is(err, errInstallationCandidateConflict) {
		t.Fatalf("同一 Deployment 不得并存两个未完成候选: %v", err)
	}
	cancelled, err := service.CancelPluginInstallationCandidate(requester, candidate.ID)
	if err != nil || cancelled.Status != plugininstallation.CandidateCancelled || cancelled.CancelledBy != "carol" {
		t.Fatalf("取消候选失败: %+v err=%v", cancelled, err)
	}
	again, err := service.CancelPluginInstallationCandidate(requester, candidate.ID)
	if err != nil || again.Status != plugininstallation.CandidateCancelled {
		t.Fatalf("重复取消必须幂等: %+v err=%v", again, err)
	}
	revisions, _ := service.ListServiceRevisions(requester)
	if len(revisions) != 1 || revisions[0].ID != 1 || !revisions[0].Active {
		t.Fatalf("取消必须原子移除未提交服务草稿: %+v", revisions)
	}
	items, err := service.ListPluginInstallationCandidates(requester)
	if err != nil || len(items) != 1 || items[0].Status != plugininstallation.CandidateCancelled {
		t.Fatalf("取消审计候选必须保留: %+v err=%v", items, err)
	}
	reopened, err := openTestService(testStateFile(service))
	if err != nil {
		t.Fatalf("重启后安装候选状态必须可验证恢复: %v", err)
	}
	restored, err := reopened.GetPluginInstallationCandidate(requester, candidate.ID)
	if err != nil || restored.Status != plugininstallation.CandidateCancelled {
		t.Fatalf("重启后取消状态丢失: %+v err=%v", restored, err)
	}
}

func TestPluginInstallationCandidateProjectsPlannerDriftAsStale(t *testing.T) {
	service, host, requester := publishedIntentService(t)
	requester.Scene = "portal.bff"
	candidate, err := service.CreatePluginInstallationCandidate(context.Background(), host, requester, plugininstallation.SourceController, upgradeInstallationRequest())
	if err != nil {
		t.Fatal(err)
	}
	host.plannerGeneration = '2'
	if _, err := service.SubmitPluginInstallationCandidate(context.Background(), host, requester, candidate.ID); !errors.Is(err, errPlanStale) {
		t.Fatalf("Catalog/Planner 漂移必须阻止候选提交: %v", err)
	}
	stale, err := service.GetPluginInstallationCandidate(requester, candidate.ID)
	if err != nil || stale.Status != plugininstallation.CandidateStale {
		t.Fatalf("候选没有投影既有 stale 状态: %+v err=%v", stale, err)
	}
}

func TestPluginInstallationTargetsExposeOnlyMinimalActiveApplicationIdentity(t *testing.T) {
	service, _, requester := publishedIntentService(t)
	targets, err := service.ListPluginInstallationTargets(requester)
	if err != nil || len(targets) != 1 {
		t.Fatalf("安装目标投影失败: %+v err=%v", targets, err)
	}
	if targets[0].Target.Kernel != "backend" || targets[0].Target.Deployment != "agent-services" || targets[0].Target.UnitID != "api" || targets[0].ActiveRevision != 1 || targets[0].ServiceClass != "application.backend" {
		t.Fatalf("安装目标泄露或缺少必要身份: %+v", targets[0])
	}
}

func upgradeInstallationRequest() plugininstallation.PreviewRequest {
	return plugininstallation.PreviewRequest{
		Version:       plugininstallation.ProtocolVersion,
		Target:        plugininstallation.Target{Kernel: "backend", Deployment: "agent-services", UnitID: "api"},
		PortalTargets: []string{},
		Change: plugininstallation.Change{
			Action: plugininstallation.ActionUpgrade, PluginID: "cn.vastplan.product.agent.api",
			Requirement: &pluginv1.ArtifactRequirement{PluginID: "cn.vastplan.product.agent.api", Constraint: "=2.0.0", Channel: "stable"},
		},
		ExpectedActiveRevision: 1,
	}
}
