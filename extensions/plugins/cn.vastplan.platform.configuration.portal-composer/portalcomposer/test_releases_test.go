package portalcomposer

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
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
	if err != nil || release.Status != portalapi.TestReleaseReady || release.PreviousActivationID != first.ID || release.CandidateActivationID == 0 || release.CandidateApplicationRevisionID == 0 {
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
	if err != nil || len(activations) != 2 || activations[0].ID != release.CandidateActivationID || activations[0].Status != portalapi.ActivationCurrent || activations[1].Status != portalapi.ActivationSuperseded {
		t.Fatalf("候选 Activation 未以 CAS 成为当前版本: %+v err=%v", activations, err)
	}
	if got := activations[0].Spec.Resolution.PluginOrigins[binding.PluginID]; got != "application" {
		t.Fatalf("测试发布改变了插件所有权: %q", got)
	}
	if activations[0].Spec.Plugins[len(activations[0].Spec.Plugins)-1].Version != request.Receipt.Ref.Version {
		t.Fatalf("候选未锁定测试版本: %+v", activations[0].Spec.Plugins)
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
	if err != nil || release.Status != portalapi.TestReleaseReady || release.CandidateProfileRevisionID == first.ProfileRevisionID || release.CandidateBindingRevisionID == first.BindingRevisionID || release.CandidateApplicationRevisionID != first.ApplicationRevisionID {
		t.Fatalf("平台插件应使用专用测试 Profile/Binding revisions: release=%+v err=%v", release, err)
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
	if _, err = service.Approve(context.Background(), approver, draft.ID); err != nil {
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
