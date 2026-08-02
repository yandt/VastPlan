package portalcomposer

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

type acceptingTestCatalog struct {
	reject       error
	referenceErr error
	calls        int
	snapshots    []pluginv1.ArtifactReferenceSnapshot
}

func portalTestReceipt(ref pluginv1.ArtifactRef, sha256 string, revision uint64) artifactrepositoryv1.Receipt {
	return artifactrepositoryv1.Receipt{
		SchemaVersion: 1, RepositoryID: "local-testing", Protocol: artifactrepositoryv1.ProtocolLocalTest,
		ProfileDigest: strings.Repeat("d", 64), Ref: ref, SHA256: sha256, Revision: revision,
	}
}

func (*acceptingTestCatalog) ValidatePortal(context.Context, string, portalapi.PortalSpec) error {
	return nil
}
func (*acceptingTestCatalog) MaterializePortal(context.Context, string, portalapi.PortalSpec) ([]pluginv1.ArtifactReference, error) {
	return []pluginv1.ArtifactReference{}, nil
}
func (c *acceptingTestCatalog) PublishReferenceSnapshot(_ context.Context, value pluginv1.ArtifactReferenceSnapshot) error {
	if c.referenceErr != nil {
		return c.referenceErr
	}
	c.snapshots = append(c.snapshots, value)
	return nil
}

func TestFrontendTestReleaseReferenceProtectionFailsClosed(t *testing.T) {
	catalog := &acceptingTestCatalog{}
	service, err := openTestService(filepath.Join(t.TempDir(), "portals.json"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	}
	author, approver, publisher := principal("author", "portal.compose"), principal("approver", "portal.approve"), principal("publisher", "portal.publish")
	admin := principal("admin", "portal.compose")
	publishTestPortalApplication(t, service, author, approver, publisher)
	zero := int64(0)
	binding, err := service.PutTestTargetBinding(context.Background(), admin, "admin-ui", portalapi.PutTestTargetBindingRequest{
		Scope: portalapi.TestTargetApplicationPlugin, PortalID: "admin", PluginID: "cn.vastplan.product.frontend.admin",
		AllowedPublishers: []string{"vastplan"}, Enabled: true, IfVersion: &zero,
	})
	if err != nil {
		t.Fatal(err)
	}
	catalog.referenceErr = errors.New("repository unavailable")
	release, err := service.CreateTestRelease(context.Background(), publisher, portalapi.CreateTestReleaseRequest{
		BindingID: binding.ID, Receipt: portalTestReceipt(pluginv1.ArtifactRef{PluginID: binding.PluginID, Version: "1.1.0-dev.20260721.9.abcdef0", Channel: "testing"}, strings.Repeat("f", 64), 17),
	})
	if err != nil || release.Status != portalapi.TestReleaseFailed || release.ErrorCode != "platform.portal_test_release.reference_protection_failed" || catalog.calls != 0 {
		t.Fatalf("引用保护失败必须在可信目录验证和候选激活前 fail-closed: %+v err=%v calls=%d", release, err, catalog.calls)
	}
}
func (c *acceptingTestCatalog) ValidateTestArtifact(_ context.Context, _ string, request portalapi.CreateTestReleaseRequest, publishers []string) error {
	c.calls++
	if c.reject != nil {
		return c.reject
	}
	if request.Receipt.Revision != 17 || request.Receipt.Ref.Channel != "testing" || len(publishers) != 1 || publishers[0] != "vastplan" {
		return errors.New("unexpected exact receipt")
	}
	return nil
}

func TestFrontendTestReleaseReusesImmutableApplicationAndActivation(t *testing.T) {
	catalog := &acceptingTestCatalog{}
	service, err := openTestService(filepath.Join(t.TempDir(), "portals.json"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	}
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	admin := principal("admin", "portal.compose")
	first := publishTestPortalApplication(t, service, author, approver, publisher)
	zero := int64(0)
	binding, err := service.PutTestTargetBinding(context.Background(), admin, "admin-ui", portalapi.PutTestTargetBindingRequest{
		Scope: portalapi.TestTargetApplicationPlugin, PortalID: "admin", PluginID: "cn.vastplan.product.frontend.admin",
		AllowedPublishers: []string{"vastplan"}, Enabled: true, IfVersion: &zero,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := portalapi.CreateTestReleaseRequest{
		BindingID: binding.ID,
		Receipt:   portalTestReceipt(pluginv1.ArtifactRef{PluginID: binding.PluginID, Version: "1.1.0-dev.20260721.1.abcdef0", Channel: "testing"}, strings.Repeat("a", 64), 17),
	}
	release, err := service.CreateTestRelease(context.Background(), publisher, request)
	if err != nil || release.Status != portalapi.TestReleaseReady || release.PreviousReleaseID != first.ID || release.CandidateReleaseID == 0 || release.CandidatePortalVersionID == 0 {
		t.Fatalf("Frontend Test Release 未完成: release=%+v err=%v", release, err)
	}
	if catalog.calls != 1 {
		t.Fatalf("精确 testing 回执应验证一次: %d", catalog.calls)
	}
	var artifactLock pluginv1.ArtifactReferenceSnapshot
	var releasedLock pluginv1.ArtifactReferenceSnapshot
	for _, snapshot := range catalog.snapshots {
		if snapshot.OwnerKind == "artifact-lock" && snapshot.Generation == 1 {
			artifactLock = snapshot
		} else if snapshot.OwnerKind == "artifact-lock" && snapshot.Generation == 2 {
			releasedLock = snapshot
		}
	}
	if len(artifactLock.References) != 1 || artifactLock.References[0].Ref != request.Receipt.Ref || artifactLock.References[0].SHA256 != request.Receipt.SHA256 {
		t.Fatalf("Frontend Test Release 必须在候选激活前保护精确 testing 制品: %+v", catalog.snapshots)
	}
	if releasedLock.Generation != 2 || len(releasedLock.References) != 0 {
		t.Fatalf("Frontend Test Release 终态必须释放临时 artifact-lock: %+v", catalog.snapshots)
	}
	activations, err := service.ListActivations(context.Background(), publisher)
	if err != nil || len(activations) != 2 || activations[0].ID != release.CandidateReleaseID || activations[0].Status != portalapi.ActivationCurrent || activations[1].Status != portalapi.ActivationSuperseded {
		t.Fatalf("候选 Activation 未以 CAS 成为当前版本: %+v err=%v", activations, err)
	}
	if got := activations[0].Spec.Resolution.PluginOrigins[binding.PluginID]; got != "application" {
		t.Fatalf("测试发布改变了插件所有权: %q", got)
	}
	if activations[0].Spec.Plugins[len(activations[0].Spec.Plugins)-1].Version != request.Receipt.Ref.Version {
		t.Fatalf("候选未锁定测试版本: %+v", activations[0].Spec.Plugins)
	}
	if owner := service.state.TestVersionOwners[release.CandidatePortalVersionID]; owner != release.ID {
		t.Fatalf("测试候选必须与 Test Release 在同一次状态提交中建立归属: owner=%d release=%d", owner, release.ID)
	}
	governance, err := service.PortalGovernance(differentSubjectTestContext(), admin)
	if err != nil || len(governance.Portals) != 1 {
		t.Fatalf("读取 Portal 聚合失败: %+v err=%v", governance, err)
	}
	portal := governance.Portals[0]
	if portal.PublishedPublication == nil || portal.PublishedPublication.ID == release.CandidatePortalVersionID || len(portal.Releases) != 1 || portal.CurrentReleaseID != first.ID {
		t.Fatalf("Test Release 不得进入正式 Portal 版本和上线谱系: %+v", portal)
	}
	if _, err := service.ReleasePortalVersion(context.Background(), publisher, portal.ID, portalapi.PortalReleaseRequest{PortalVersionID: release.CandidatePortalVersionID, ExpectedCurrentReleaseID: release.CandidateReleaseID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("测试候选不得通过普通 PortalRelease 晋级: %v", err)
	}
	next, err := service.CreatePortalVersion(context.Background(), author, portal.ID, latestPortalVersionForTest(t, service, author.TenantID, portal.ID).Configuration)
	if err != nil || next.Number != 2 {
		t.Fatalf("隔离的测试候选不得占用正式版本号或阻塞新草稿: %+v err=%v", next, err)
	}
	for _, step := range []struct {
		principal portalapi.Principal
		action    string
	}{{author, "submit"}, {approver, "approve"}, {publisher, "publish"}} {
		next, err = service.TransitionPortalVersion(differentSubjectTestContext(), step.principal, portal.ID, next.ID, step.action)
		if err != nil {
			t.Fatalf("正式版本推进失败: action=%s err=%v", step.action, err)
		}
	}
	stable, err := service.ReleasePortalVersion(context.Background(), publisher, portal.ID, portalapi.PortalReleaseRequest{
		PortalVersionID: next.ID, ExpectedCurrentReleaseID: first.ID, Reason: "replace test overlay with stable release",
	})
	if err != nil || stable.Status != portalapi.ActivationCurrent || stable.PreviousReleaseID != first.ID {
		t.Fatalf("正式上线必须基于正式基线 CAS 并替换测试覆盖: release=%+v err=%v", stable, err)
	}
	updated, err := service.portalTestRelease(publisher, release.ID)
	if err != nil || updated.Status != portalapi.TestReleaseSuperseded {
		t.Fatalf("被正式上线替换的测试发布必须标记为 Superseded: release=%+v err=%v", updated, err)
	}
	if current := service.currentActivationIDLocked(author.TenantID, portal.ID); current != stable.ID {
		t.Fatalf("正式上线后运行态必须指向新 Release: got=%d want=%d", current, stable.ID)
	}
	secondTest, err := service.CreateTestRelease(context.Background(), publisher, portalapi.CreateTestReleaseRequest{
		BindingID: binding.ID,
		Receipt:   portalTestReceipt(pluginv1.ArtifactRef{PluginID: binding.PluginID, Version: "1.1.0-dev.20260721.2.abcdef0", Channel: "testing"}, strings.Repeat("b", 64), 17),
	})
	if err != nil || secondTest.Status != portalapi.TestReleaseReady {
		t.Fatalf("第二次 Frontend Test Release 未完成: release=%+v err=%v", secondTest, err)
	}
	rolledBack, err := service.RollbackPortalRelease(context.Background(), publisher, portal.ID, first.ID, stable.ID, "restore stable baseline")
	if err != nil || rolledBack.Status != portalapi.ActivationCurrent || rolledBack.PreviousReleaseID != stable.ID {
		t.Fatalf("正式回滚必须基于正式基线 CAS 并替换测试覆盖: release=%+v err=%v", rolledBack, err)
	}
	updated, err = service.portalTestRelease(publisher, secondTest.ID)
	if err != nil || updated.Status != portalapi.TestReleaseSuperseded {
		t.Fatalf("被正式回滚替换的测试发布必须标记为 Superseded: release=%+v err=%v", updated, err)
	}
}

func TestFrontendTestReleaseCandidateAssociationIsAtomic(t *testing.T) {
	catalog := &acceptingTestCatalog{}
	service, err := openTestService(filepath.Join(t.TempDir(), "portals.json"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	}
	author, approver := principal("author", "portal.compose"), principal("approver", "portal.approve")
	publisher, admin := principal("publisher", "portal.publish"), principal("admin", "portal.compose")
	publishTestPortalApplication(t, service, author, approver, publisher)
	zero := int64(0)
	binding, err := service.PutTestTargetBinding(context.Background(), admin, "admin-ui", portalapi.PutTestTargetBindingRequest{
		Scope: portalapi.TestTargetApplicationPlugin, PortalID: "admin", PluginID: "cn.vastplan.product.frontend.admin",
		AllowedPublishers: []string{"vastplan"}, Enabled: true, IfVersion: &zero,
	})
	if err != nil {
		t.Fatal(err)
	}
	stableVersions := len(service.state.Revisions)
	persist := service.testSave
	service.testSave = func(value state) error {
		if len(value.TestVersionOwners) != 0 {
			return errors.New("injected candidate commit failure")
		}
		return persist(value)
	}
	release, err := service.CreateTestRelease(context.Background(), publisher, portalapi.CreateTestReleaseRequest{
		BindingID: binding.ID,
		Receipt:   portalTestReceipt(pluginv1.ArtifactRef{PluginID: binding.PluginID, Version: "1.1.0-dev.20260721.8.abcdef0", Channel: "testing"}, strings.Repeat("8", 64), 17),
	})
	if err != nil || release.Status != portalapi.TestReleaseFailed || release.CandidatePortalVersionID != 0 {
		t.Fatalf("候选原子提交失败必须形成已关联的失败结果且不暴露候选: %+v err=%v", release, err)
	}
	if len(service.state.TestVersionOwners) != 0 || len(service.state.Revisions) != stableVersions {
		t.Fatalf("候选与 Test Release 关联写入失败后不得留下孤儿版本: owners=%v revisions=%d", service.state.TestVersionOwners, len(service.state.Revisions))
	}
}

func TestFrontendTestReleaseRejectsProfileSlotAndPreservesCurrentActivation(t *testing.T) {
	catalog := &acceptingTestCatalog{reject: errors.New("catalog rejected")}
	service, err := openTestService(filepath.Join(t.TempDir(), "portals.json"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	}
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	admin := principal("admin", "portal.compose")
	first := publishTestPortalApplication(t, service, author, approver, publisher)
	zero := int64(0)
	if _, err := service.PutTestTargetBinding(context.Background(), admin, "shell", portalapi.PutTestTargetBindingRequest{
		Scope: portalapi.TestTargetApplicationPlugin, PortalID: "admin", PluginID: "cn.vastplan.foundation.frontend.structure.shell",
		AllowedPublishers: []string{"vastplan"}, Enabled: true, IfVersion: &zero,
	}); err == nil {
		t.Fatal("Platform Profile 的 Shell 插件不得绑定到 Application Test Release")
	}
	binding, err := service.PutTestTargetBinding(context.Background(), admin, "admin-ui", portalapi.PutTestTargetBindingRequest{
		Scope: portalapi.TestTargetApplicationPlugin, PortalID: "admin", PluginID: "cn.vastplan.product.frontend.admin",
		AllowedPublishers: []string{"vastplan"}, Enabled: true, IfVersion: &zero,
	})
	if err != nil {
		t.Fatal(err)
	}
	release, err := service.CreateTestRelease(context.Background(), publisher, portalapi.CreateTestReleaseRequest{
		BindingID: binding.ID, Receipt: portalTestReceipt(pluginv1.ArtifactRef{PluginID: binding.PluginID, Version: "1.1.0-dev.20260721.2.abcdef0", Channel: "testing"}, strings.Repeat("b", 64), 18),
	})
	if err != nil || release.Status != portalapi.TestReleaseFailed || release.RollbackRequired {
		t.Fatalf("目录拒绝应在 Activation 前安全失败: release=%+v err=%v", release, err)
	}
	if current := service.currentActivationIDLocked("tenant-a", "admin"); current != first.ID {
		t.Fatalf("失败候选改变了当前 Activation: got=%d want=%d", current, first.ID)
	}
}

func TestFrontendTestReleaseRestartPersistsFailClosedRecovery(t *testing.T) {
	catalog := &acceptingTestCatalog{}
	stateFile := filepath.Join(t.TempDir(), "portals.json")
	service, err := openTestService(stateFile, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	}
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	admin := principal("admin", "portal.compose")
	publishTestPortalApplication(t, service, author, approver, publisher)
	zero := int64(0)
	binding, err := service.PutTestTargetBinding(context.Background(), admin, "admin-ui", portalapi.PutTestTargetBindingRequest{
		Scope: portalapi.TestTargetApplicationPlugin, PortalID: "admin", PluginID: "cn.vastplan.product.frontend.admin",
		AllowedPublishers: []string{"vastplan"}, Enabled: true, IfVersion: &zero,
	})
	if err != nil {
		t.Fatal(err)
	}
	release, err := service.CreateTestRelease(context.Background(), publisher, portalapi.CreateTestReleaseRequest{
		BindingID: binding.ID, Receipt: portalTestReceipt(pluginv1.ArtifactRef{PluginID: binding.PluginID, Version: "1.2.0-dev.20260721.3.abcdef0", Channel: "testing"}, strings.Repeat("d", 64), 17),
	})
	if err != nil || release.Status != portalapi.TestReleaseReady {
		t.Fatalf("测试前置发布失败: %+v %v", release, err)
	}
	service.state.TestReleases[0].Status = portalapi.TestReleaseActivating
	if err := service.save(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTestService(stateFile, catalog)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.ListTestReleases(context.Background(), publisher)
	if err != nil || len(recovered) != 1 || recovered[0].Status != portalapi.TestReleaseFailed || !recovered[0].RollbackRequired {
		t.Fatalf("非终态重启必须 fail-closed 并要求回滚: %+v %v", recovered, err)
	}
	second, err := openTestService(stateFile, catalog)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := second.ListTestReleases(context.Background(), publisher)
	if err != nil || len(persisted) != 1 || persisted[0].Status != portalapi.TestReleaseFailed {
		t.Fatalf("恢复结果必须立即持久化: %+v %v", persisted, err)
	}
}

func TestFrontendProfileTestReleaseCreatesDedicatedProfileAndBindingRevisions(t *testing.T) {
	catalog := &acceptingTestCatalog{}
	service, err := openTestService(filepath.Join(t.TempDir(), "portals.json"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	}
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	admin := principal("admin", "portal.compose")
	first := publishTestPortalApplication(t, service, author, approver, publisher)
	pluginID := "cn.vastplan.foundation.frontend.workflow.workbench"
	zero := int64(0)
	binding, err := service.PutTestTargetBinding(context.Background(), admin, "workbench", portalapi.PutTestTargetBindingRequest{
		Scope: portalapi.TestTargetPlatformProfilePlugin, PortalID: "admin", PluginID: pluginID,
		AllowedPublishers: []string{"vastplan"}, Enabled: true, IfVersion: &zero,
	})
	if err != nil {
		t.Fatal(err)
	}
	release, err := service.CreateTestRelease(context.Background(), publisher, portalapi.CreateTestReleaseRequest{
		BindingID: binding.ID, Receipt: portalTestReceipt(pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.1.0-dev.20260721.4.abcdef0", Channel: "testing"}, strings.Repeat("e", 64), 17),
	})
	if err != nil || release.Status != portalapi.TestReleaseReady || release.CandidatePortalVersionID == first.ApplicationRevisionID {
		t.Fatalf("平台插件应形成完整的专用测试 PortalVersion: release=%+v err=%v", release, err)
	}
	activations, err := service.ListActivations(context.Background(), publisher)
	if err != nil || len(activations) != 2 || activations[0].Spec.Workbench.Version != "1.1.0-dev.20260721.4.abcdef0" || activations[0].Spec.Workbench.Channel != "testing" {
		t.Fatalf("测试 Profile 未锁定候选 Workbench: %+v %v", activations, err)
	}
	if service.state.Profiles[len(service.state.Profiles)-1].TenantID != "tenant-a" {
		t.Fatal("测试 Profile 必须属于目标 tenant，不得改写全局 Profile")
	}
}

func publishTestPortalApplication(t *testing.T, service *Service, author, approver, publisher portalapi.Principal) portalapi.PortalActivation {
	t.Helper()
	draft, err := service.CreateDraft(context.Background(), author, testComposition("/"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Submit(context.Background(), author, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Approve(differentSubjectTestContext(), approver, draft.ID); err != nil {
		t.Fatal(err)
	}
	published, err := service.Publish(context.Background(), publisher, draft.ID, portalapi.PublishRequest{})
	if err != nil {
		t.Fatal(err)
	}
	activation, err := service.Activate(context.Background(), publisher, activationRequest(service, published, 0))
	if err != nil || activation.Status != portalapi.ActivationCurrent {
		t.Fatalf("初始 Portal 激活失败: %+v %v", activation, err)
	}
	return activation
}
