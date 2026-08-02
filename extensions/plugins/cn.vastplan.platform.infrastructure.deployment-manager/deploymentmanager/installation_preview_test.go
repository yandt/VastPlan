package deploymentmanager

import (
	"context"
	"errors"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

func TestPluginInstallationPreviewReusesPlannerWithoutPersistingRevision(t *testing.T) {
	service, host, call := publishedIntentService(t)
	call.Scene = "portal.bff"
	request := plugininstallation.PreviewRequest{
		Version: plugininstallation.ProtocolVersion,
		Target:  plugininstallation.Target{Kernel: "backend", Deployment: "agent-services", UnitID: "api"},
		Change: plugininstallation.Change{
			Action: plugininstallation.ActionUpgrade, PluginID: "cn.vastplan.product.agent.api",
			Requirement: &pluginv1.ArtifactRequirement{PluginID: "cn.vastplan.product.agent.api", Constraint: "=2.0.0", Channel: "stable"},
		},
		ExpectedActiveRevision: 1,
	}
	preview, err := service.PreviewPluginInstallation(context.Background(), host, call, plugininstallation.SourceController, request)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Source != plugininstallation.SourceController || preview.ActiveRevision != 1 || preview.CandidateRevision != 2 || preview.RepositoryRevision != 5 || preview.PreviewDigest == "" {
		t.Fatalf("安装预览没有绑定活动修订、Catalog 与内核预览: %+v", preview)
	}
	if len(preview.Changes) != 1 || preview.Changes[0].Kind != plugininstallation.PackageUpdated || !preview.Changes[0].Root || preview.Changes[0].After.Ref.Version != "2.0.0" {
		t.Fatalf("安装差异不正确: %+v", preview.Changes)
	}
	if !preview.Impact.RequiresApproval || preview.Impact.KernelRestartRequired || preview.Impact.ApplyStrategy != plugininstallation.ApplyServiceGeneration {
		t.Fatalf("生产预览影响语义不正确: %+v", preview.Impact)
	}
	revisions, err := service.ListServiceRevisions(call)
	if err != nil || len(revisions) != 1 || revisions[0].ID != 1 {
		t.Fatalf("只读预览不得创建服务修订: %+v err=%v", revisions, err)
	}
}

func TestPluginInstallationPreviewEnforcesTrustedSourceAdapters(t *testing.T) {
	service, host, _ := publishedIntentService(t)
	request := plugininstallation.PreviewRequest{
		Version: plugininstallation.ProtocolVersion,
		Target:  plugininstallation.Target{Kernel: "backend", Deployment: "agent-services", UnitID: "api"},
		Change: plugininstallation.Change{
			Action: plugininstallation.ActionUpgrade, PluginID: "cn.vastplan.product.agent.api",
			Requirement: &pluginv1.ArtifactRequirement{PluginID: "cn.vastplan.product.agent.api", Constraint: "=2.0.0", Channel: "stable"},
		},
	}
	selfService := userCall("tenant-a", "alice")
	if _, err := service.PreviewPluginInstallation(context.Background(), host, selfService, plugininstallation.SourceSelfService, request); !errors.Is(err, plugininstallation.ErrUntrustedSource) {
		t.Fatalf("服务自助入口必须来自可信 Portal BFF: %v", err)
	}
	selfService.Scene = "portal.bff"
	preview, err := service.PreviewPluginInstallation(context.Background(), host, selfService, plugininstallation.SourceSelfService, request)
	if err != nil || preview.Source != plugininstallation.SourceSelfService || !preview.Impact.RequiresApproval {
		t.Fatalf("服务自助适配器没有进入统一预览链: %+v err=%v", preview, err)
	}
	if _, err = service.PutTestTargetBinding(selfService, "agent-api-dev", platformadminapi.PutTestTargetBindingRequest{
		Kind: platformadminapi.TestTargetBackend, Deployment: "agent-services", UnitID: "api",
		PluginID: "cn.vastplan.product.agent.api", AllowedPublishers: []string{"vastplan"}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	development := &contractv1.CallContext{
		TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "platform-dev/watch"},
	}
	preview, err = service.PreviewPluginInstallation(context.Background(), host, development, plugininstallation.SourceDevelopment, request)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Impact.RequiresApproval || preview.Source != plugininstallation.SourceDevelopment {
		t.Fatalf("可信开发来源应形成无需人工审批的同链预览: %+v", preview)
	}
}

func TestPluginInstallationPreviewRejectsPlatformPlugin(t *testing.T) {
	lock := &pluginv1.ArtifactLock{Packages: []pluginv1.ArtifactLockPackage{{
		Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.platform.example.service", Version: "1.0.0", Channel: "stable"}, Publisher: "vastplan",
	}}}
	if err := requireApplicationManagedPlugin("cn.vastplan.platform.example.service", plugininstallation.SourceController, lock); err == nil {
		t.Fatal("服务安装入口不得选择 platform 插件")
	}
}

func TestPluginInstallationDiffIncludesTransitiveDependencies(t *testing.T) {
	before := &pluginv1.ArtifactLock{
		Roots: []pluginv1.ArtifactRequirement{{PluginID: "cn.example.app", Constraint: "=1.0.0"}},
		Packages: []pluginv1.ArtifactLockPackage{
			{Ref: pluginv1.ArtifactRef{PluginID: "cn.example.app", Version: "1.0.0", Channel: "stable"}, SHA256: "old-app"},
			{Ref: pluginv1.ArtifactRef{PluginID: "cn.example.old-dependency", Version: "1.0.0", Channel: "stable"}, SHA256: "old-dependency"},
		},
	}
	after := &pluginv1.ArtifactLock{
		Roots: []pluginv1.ArtifactRequirement{{PluginID: "cn.example.app", Constraint: "=2.0.0"}},
		Packages: []pluginv1.ArtifactLockPackage{
			{Ref: pluginv1.ArtifactRef{PluginID: "cn.example.app", Version: "2.0.0", Channel: "stable"}, SHA256: "new-app"},
			{Ref: pluginv1.ArtifactRef{PluginID: "cn.example.new-dependency", Version: "1.0.0", Channel: "stable"}, SHA256: "new-dependency"},
		},
	}
	changes := diffArtifactLocks(before, after)
	if len(changes) != 3 || changes[0].PluginID != "cn.example.app" || changes[0].Kind != plugininstallation.PackageUpdated || !changes[0].Root ||
		changes[1].PluginID != "cn.example.new-dependency" || changes[1].Kind != plugininstallation.PackageAdded || changes[1].Root ||
		changes[2].PluginID != "cn.example.old-dependency" || changes[2].Kind != plugininstallation.PackageRemoved || changes[2].Root {
		t.Fatalf("传递依赖差异不完整或不稳定: %+v", changes)
	}
}

func publishedIntentService(t *testing.T) (*Service, *intentWorkflowHost, *contractv1.CallContext) {
	t.Helper()
	service, err := openTestService(t.TempDir() + "/deployment-manager.json")
	if err != nil {
		t.Fatal(err)
	}
	host := &intentWorkflowHost{profile: intentPlatformProfile(), plannerGeneration: '1'}
	alice, bob, carol := userCall("tenant-a", "alice"), userCall("tenant-a", "bob"), userCall("tenant-a", "carol")
	draft, err := service.CreateIntentDraft(context.Background(), host, alice, intentFixture())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SubmitServiceDraft(context.Background(), host, alice, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApproveServiceRevision(context.Background(), host, bob, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.PublishServiceRevision(context.Background(), host, carol, draft.ID); err != nil {
		t.Fatal(err)
	}
	return service, host, carol
}
