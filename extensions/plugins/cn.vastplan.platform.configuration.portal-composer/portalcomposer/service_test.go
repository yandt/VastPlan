package portalcomposer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type acceptingCatalog struct{}

func TestDescriptorMatchesSignedManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	runtimeDescriptors := map[string][]byte{portalapi.ComposerCapability: Descriptor(), portalapi.PreferenceCapability: PreferenceDescriptor()}
	if len(contributions) != len(runtimeDescriptors) {
		t.Fatalf("运行时 contribution 数与签名 Manifest 不一致: signed=%d runtime=%d", len(contributions), len(runtimeDescriptors))
	}
	for _, contribution := range contributions {
		var signed, runtime any
		raw, ok := runtimeDescriptors[contribution.ID]
		if !ok || json.Unmarshal(contribution.Descriptor, &signed) != nil || json.Unmarshal(raw, &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
			t.Fatalf("运行时 descriptor 与签名 Manifest 不一致: %s\nsigned=%s\nruntime=%s", contribution.ID, contribution.Descriptor, raw)
		}
	}
}

func TestComposerCapabilityContractClassifiesAndGuardsEveryUserOperation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := pluginv1.ManifestToolCapabilityContracts(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range contracts {
		if contract.Capability != portalapi.ComposerCapability {
			continue
		}
		if len(contract.Operations) != len(signedToolOperationNames(portalapi.ComposerCapability)) {
			t.Fatalf("生成操作与签名契约数量不一致: signed=%d generated=%d", len(contract.Operations), len(signedToolOperationNames(portalapi.ComposerCapability)))
		}
		for _, operation := range contract.Operations {
			if operation.Audience != "user" || operation.Guard == nil {
				t.Fatalf("用户操作必须完成受众和权限闭包: %+v", operation)
			}
			if operation.Name == "transitionProfile" || operation.Name == "transitionBinding" {
				t.Fatalf("权限依赖 payload 的旧 transition 操作不得保留: %s", operation.Name)
			}
		}
		return
	}
	t.Fatal("签名 Manifest 缺少 Portal Composer Capability Contract")
}

func (acceptingCatalog) ValidatePortal(context.Context, string, portalapi.PortalSpec) error {
	return nil
}
func (acceptingCatalog) MaterializePortal(context.Context, string, portalapi.PortalSpec) ([]pluginv1.ArtifactReference, error) {
	return []pluginv1.ArtifactReference{}, nil
}
func (acceptingCatalog) PublishReferenceSnapshot(context.Context, pluginv1.ArtifactReferenceSnapshot) error {
	return nil
}

type recordingReferenceCatalog struct {
	snapshots []pluginv1.ArtifactReferenceSnapshot
	calls     int
	failAt    int
}

func (*recordingReferenceCatalog) ValidatePortal(context.Context, string, portalapi.PortalSpec) error {
	return nil
}
func (*recordingReferenceCatalog) MaterializePortal(_ context.Context, _ string, spec portalapi.PortalSpec) ([]pluginv1.ArtifactReference, error) {
	values := make([]pluginv1.ArtifactReference, 0, len(spec.Plugins))
	for _, ref := range spec.Plugins {
		channel := ref.Channel
		if channel == "" {
			channel = "stable"
		}
		values = append(values, pluginv1.ArtifactReference{Ref: pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: channel}, SHA256: strings.Repeat("a", 64), Purpose: "candidate"})
	}
	return values, nil
}
func (c *recordingReferenceCatalog) PublishReferenceSnapshot(_ context.Context, value pluginv1.ArtifactReferenceSnapshot) error {
	c.calls++
	if c.calls == c.failAt {
		return errors.New("repository temporarily unavailable")
	}
	c.snapshots = append(c.snapshots, value)
	return nil
}

func principal(id string, roles ...string) portalapi.Principal {
	return portalapi.Principal{ID: id, TenantID: "tenant-a", Roles: roles}
}
func spec(route string) frontendcompositionv1.ApplicationComposition {
	value := testComposition(route)
	value.Plugins = []frontendcompositionv1.PluginRef{}
	return value
}
func newTestService(t *testing.T) *Service {
	t.Helper()
	s, err := openTestService(filepath.Join(t.TempDir(), "portals.json"), acceptingCatalog{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	}
	return s
}

func latestPortalVersionForTest(t *testing.T, service *Service, tenantID, portalID string) portalapi.PortalVersion {
	t.Helper()
	service.mu.Lock()
	defer service.mu.Unlock()
	var latest *portalapi.Revision
	for index := range service.state.Revisions {
		revision := &service.state.Revisions[index]
		if revision.TenantID != tenantID || revision.PortalID != portalID || service.isTestVersionLocked(revision.ID) {
			continue
		}
		if latest == nil || revision.Number > latest.Number {
			latest = revision
		}
	}
	if latest == nil {
		t.Fatalf("Portal %s 没有正式配置记录", portalID)
	}
	version, err := service.portalVersionLocked(tenantID, *latest)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func TestAuthorizedBoundaryDoesNotRepeatLegacyTokenRolePolicy(t *testing.T) {
	service := newTestService(t)
	// Kernel Enforcer has already authorized this operation from the online
	// Role/Binding policy. The domain service must not require legacy role
	// strings to be copied into the identity token as a second policy source.
	trusted := principal("online-role-user")
	if _, err := service.CreateDraft(context.Background(), trusted, spec("/online-role")); err != nil {
		t.Fatalf("已在 Kernel 边界授权的可信主体不应被旧 Token 角色表拒绝: %v", err)
	}
}

func TestGovernedPublishRequiresDifferentApproverAndPersistsAudit(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "portals.json")
	s, err := openTestService(stateFile, acceptingCatalog{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	}
	author := principal("author", "portal.compose", "portal.approve")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	draft, err := s.CreateDraft(context.Background(), author, spec("/"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(context.Background(), author, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(context.Background(), author, draft.ID); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("自审必须被拒绝: %v", err)
	}
	if _, err := s.Approve(context.Background(), approver, draft.ID); err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(context.Background(), publisher, draft.ID, portalapi.PublishRequest{})
	if err != nil || published.Status != portalapi.StatusPublished {
		t.Fatalf("发布失败: %+v %v", published, err)
	}
	activation, err := s.Activate(context.Background(), publisher, activationRequest(s, published, 0))
	if err != nil || activation.Status != portalapi.ActivationCurrent {
		t.Fatalf("激活失败: %+v %v", activation, err)
	}
	audit, err := s.Audit(context.Background(), publisher, draft.PortalID, draft.ID)
	if err != nil || len(audit) != 4 {
		t.Fatalf("审计事件应完整保留: %+v %v", audit, err)
	}
	if reopened, err := openTestService(stateFile, acceptingCatalog{}); err != nil {
		t.Fatal(err)
	} else if err := reopened.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	} else if got, err := reopened.ListActivations(context.Background(), publisher); err != nil || len(got) != 1 || got[0].Status != portalapi.ActivationCurrent {
		t.Fatalf("持久化状态错误: %+v %v", got, err)
	}
}

func TestActivationReferenceOutboxRetriesAfterRepositoryRecovery(t *testing.T) {
	catalog := &recordingReferenceCatalog{failAt: 2}
	s, err := openTestService(filepath.Join(t.TempDir(), "portals.json"), catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BindPlatformCatalog(testPlatformCatalog()); err != nil {
		t.Fatal(err)
	}
	author, approver, publisher := principal("author", "portal.compose"), principal("approver", "portal.approve"), principal("publisher", "portal.publish")
	draft, err := s.CreateDraft(context.Background(), author, spec("/"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Submit(context.Background(), author, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Approve(context.Background(), approver, draft.ID); err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(context.Background(), publisher, draft.ID, portalapi.PublishRequest{})
	if err != nil {
		t.Fatal(err)
	}
	activation, err := s.Activate(context.Background(), publisher, activationRequest(s, published, 0))
	if err != nil || !activation.ReferencePending || len(activation.ArtifactReferences) == 0 {
		t.Fatalf("引用仓库瞬时失败不得撤销已完成 Activation，且必须留下精确 outbox: %+v err=%v", activation, err)
	}
	catalog.failAt = 0
	activations, err := s.ListActivations(context.Background(), publisher)
	if err != nil || len(activations) != 1 || activations[0].ReferencePending {
		t.Fatalf("仓库恢复后引用 outbox 未收敛: %+v err=%v", activations, err)
	}
	if len(catalog.snapshots) != 3 || catalog.snapshots[0].OwnerKind != "portal-activation" || catalog.snapshots[0].Generation != 1 || catalog.snapshots[1].OwnerKind != "rollback-history" || catalog.snapshots[2].Generation != 2 {
		t.Fatalf("Portal 引用保护顺序错误: %+v", catalog.snapshots)
	}
}

func TestDraftCanBeUpdatedOnlyBeforeSubmission(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	draft, err := s.CreateDraft(context.Background(), author, spec("/old"))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.UpdateDraft(context.Background(), author, draft.ID, spec("/new"))
	if err != nil || updated.Composition.Route != "/new" || updated.Spec.Route != "/new" {
		t.Fatalf("更新草稿失败: revision=%+v err=%v", updated, err)
	}
	audit, err := s.Audit(context.Background(), author, draft.PortalID, draft.ID)
	if err != nil || len(audit) != 2 || audit[1].Action != "portal.version.updated" {
		t.Fatalf("更新草稿审计缺失: %+v %v", audit, err)
	}
	if _, err := s.Submit(context.Background(), author, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateDraft(context.Background(), author, draft.ID, spec("/forbidden")); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("提交后不得更新草稿: %v", err)
	}
}

func TestPortalIDIsUniqueAndLineageAllowsOnlyOneOpenVersion(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	configuration, err := s.configurationFromCatalog(spec("/"), author.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	portal, err := s.CreatePortal(context.Background(), author, portalapi.CreatePortalRequest{PortalID: "admin", Configuration: configuration})
	if err != nil || portal.WorkingCopy == nil || portal.WorkingCopy.Revision != 1 {
		t.Fatalf("创建 Portal 失败: %+v %v", portal, err)
	}
	initial := latestPortalVersionForTest(t, s, author.TenantID, portal.ID)
	raw, err := json.Marshal(portal)
	if err != nil || !strings.Contains(string(raw), `"releases":[]`) {
		t.Fatalf("空上线历史必须编码为 []，避免前端聚合崩溃: %s err=%v", raw, err)
	}
	if _, err := s.CreatePortal(context.Background(), author, portalapi.CreatePortalRequest{PortalID: "admin", Configuration: configuration}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("同一 tenant 的 portalId 必须唯一: %v", err)
	}
	if _, err := s.CreatePortalVersion(context.Background(), author, "admin", configuration); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("存在草稿时不得创建第二个候选版本: %v", err)
	}
	deleted, err := s.DeletePortalVersion(context.Background(), author, "admin", initial.ID)
	if err != nil || deleted.ID != initial.ID {
		t.Fatalf("删除 PortalVersion 草稿失败: %+v %v", deleted, err)
	}
}

func TestPortalAuditAndBreakGlassKeepTrustedPortalIdentity(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	configuration, err := s.configurationFromCatalog(spec("/"), author.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	portal, err := s.CreatePortal(context.Background(), author, portalapi.CreatePortalRequest{PortalID: "admin", Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	version := latestPortalVersionForTest(t, s, author.TenantID, portal.ID)
	system := portalapi.Principal{ID: "system", TenantID: author.TenantID, System: true}
	if _, err := s.breakGlassPublishPortalVersion(context.Background(), system, "forged", version.ID, "incident recovery"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("URL Portal 身份不得被版本 ID 绕过: %v", err)
	}
	published, err := s.breakGlassPublishPortalVersion(context.Background(), system, portal.ID, version.ID, "incident recovery")
	if err != nil || published.Status != portalapi.StatusPublished {
		t.Fatalf("带原因的系统 break-glass 发布失败: %+v err=%v", published, err)
	}
	audit, err := s.Audit(context.Background(), author, portal.ID, version.ID)
	if err != nil || len(audit) != 2 || audit[1].Reason != "incident recovery" || audit[1].Priority != "high" {
		t.Fatalf("break-glass 原因和高危审计未完整保留: %+v err=%v", audit, err)
	}
	if _, err := s.Audit(context.Background(), author, "forged", version.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("审计查询必须校验 Portal 路径身份: %v", err)
	}
}

func TestReleaseRejectsHistoricalPublishedVersion(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	configuration, err := s.configurationFromCatalog(spec("/"), author.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	portal, err := s.CreatePortal(context.Background(), author, portalapi.CreatePortalRequest{PortalID: "admin", Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	publish := func(version portalapi.PortalVersion) portalapi.PortalVersion {
		t.Helper()
		var transitionErr error
		for _, step := range []struct {
			principal portalapi.Principal
			action    string
		}{{author, "submit"}, {approver, "approve"}, {publisher, "publish"}} {
			version, transitionErr = s.TransitionPortalVersion(context.Background(), step.principal, portal.ID, version.ID, step.action)
			if transitionErr != nil {
				t.Fatal(transitionErr)
			}
		}
		return version
	}
	first := publish(latestPortalVersionForTest(t, s, author.TenantID, portal.ID))
	configuration.Application.Route = "/new"
	second, err := s.CreatePortalVersion(context.Background(), author, portal.ID, configuration)
	if err != nil {
		t.Fatal(err)
	}
	_ = publish(second)
	if _, err := s.ReleasePortalVersion(context.Background(), publisher, portal.ID, portalapi.PortalReleaseRequest{PortalVersionID: first.ID}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("普通上线不得用旧 Published 版本伪装回滚: %v", err)
	}
}

func TestPublishRejectsCrossPortalRouteAndBreakGlassNeedsReason(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	configuration, err := s.configurationFromCatalog(spec("/"), author.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	publish := func(id string, checkBreakGlass bool) portalapi.PortalRelease {
		candidate := cloneJSON(configuration)
		candidate.Application.ID = id
		portal, createErr := s.CreatePortal(context.Background(), author, portalapi.CreatePortalRequest{PortalID: id, Configuration: candidate})
		if createErr != nil {
			t.Fatal(createErr)
		}
		version := latestPortalVersionForTest(t, s, author.TenantID, portal.ID)
		if checkBreakGlass {
			if _, publishErr := s.Publish(context.Background(), portalapi.Principal{ID: "system", TenantID: "tenant-a", System: true}, version.ID, portalapi.PublishRequest{}); publishErr == nil {
				t.Fatal("break-glass 缺原因必须拒绝")
			}
		}
		for _, step := range []struct {
			principal portalapi.Principal
			action    string
		}{{author, "submit"}, {approver, "approve"}, {publisher, "publish"}} {
			if _, transitionErr := s.TransitionPortalVersion(context.Background(), step.principal, id, version.ID, step.action); transitionErr != nil {
				t.Fatal(transitionErr)
			}
		}
		release, releaseErr := s.ReleasePortalVersion(context.Background(), publisher, id, portalapi.PortalReleaseRequest{PortalVersionID: version.ID, ExpectedCurrentReleaseID: 0})
		if releaseErr != nil {
			t.Fatal(releaseErr)
		}
		return release
	}
	first := publish("admin", false)
	failed := publish("two", true)
	if failed.Status != portalapi.ActivationFailed || first.Status != portalapi.ActivationCurrent {
		t.Fatalf("同租户跨 Portal 路由冲突必须产生持久失败 Release: %+v", failed)
	}
	if _, err = s.Publish(context.Background(), portalapi.Principal{ID: "system", TenantID: "tenant-a", System: true}, first.PublicationID, portalapi.PublishRequest{}); err == nil {
		t.Fatal("break-glass 缺原因必须拒绝")
	}
}

func TestRollbackCreatesNewImmutableActivation(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	publish := func(expected uint64) portalapi.PortalActivation {
		t.Helper()
		draft, err := s.CreateDraft(context.Background(), author, spec("/"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Submit(context.Background(), author, draft.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Approve(context.Background(), approver, draft.ID); err != nil {
			t.Fatal(err)
		}
		published, err := s.Publish(context.Background(), publisher, draft.ID, portalapi.PublishRequest{})
		if err != nil {
			t.Fatal(err)
		}
		activation, err := s.Activate(context.Background(), publisher, activationRequest(s, published, expected))
		if err != nil || activation.Status != portalapi.ActivationCurrent {
			t.Fatalf("激活失败: %+v %v", activation, err)
		}
		return activation
	}
	first := publish(0)
	second := publish(first.ID)
	rolledBack, err := s.RollbackActivation(context.Background(), publisher, first.ID, second.ID, "恢复已验证基线")
	if err != nil || rolledBack.Status != portalapi.ActivationCurrent || rolledBack.PreviousActivationID != second.ID {
		t.Fatalf("历史 Activation 回滚失败: activation=%+v err=%v", rolledBack, err)
	}
	if _, err := s.RollbackActivation(context.Background(), publisher, rolledBack.ID, rolledBack.ID, "不能回滚当前"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("当前 Activation 不能作为回滚源: %v", err)
	}
}

func TestPublishedPortalVersionDoesNotGoLiveBeforeReleaseCAS(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	configuration, err := s.configurationFromCatalog(spec("/"), author.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	configuration.Platform.Shell.Config.DefaultTemplate = "top-navigation"
	portal, err := s.CreatePortal(context.Background(), author, portalapi.CreatePortalRequest{PortalID: "admin", Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	version := latestPortalVersionForTest(t, s, author.TenantID, portal.ID)
	for _, step := range []struct {
		principal portalapi.Principal
		action    string
	}{{author, "submit"}, {approver, "approve"}, {publisher, "publish"}} {
		version, err = s.TransitionPortalVersion(context.Background(), step.principal, portal.ID, version.ID, step.action)
		if err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.ListPortalReleases(context.Background(), publisher); err != nil || len(got) != 0 {
		t.Fatalf("Published PortalVersion 不得自动上线: %+v %v", got, err)
	}
	request := portalapi.PortalReleaseRequest{PortalVersionID: version.ID, ExpectedCurrentReleaseID: 0, Reason: "切换到顶部导航"}
	current, err := s.ReleasePortalVersion(context.Background(), publisher, portal.ID, request)
	if err != nil || current.Status != portalapi.ActivationCurrent || current.Resolved.Shell.ID != configuration.Platform.Shell.ID || current.Resolved.Shell.Config.DefaultTemplate != "top-navigation" {
		t.Fatalf("Release 未使用精确 PortalVersion: %+v %v", current, err)
	}
	if _, err := s.ReleasePortalVersion(context.Background(), publisher, portal.ID, request); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("过期 expectedCurrentReleaseId 必须被 CAS 拒绝: %v", err)
	}
}

func activationRequest(s *Service, application portalapi.Revision, expected uint64) portalapi.ActivationRequest {
	profile := s.state.Profiles[0]
	for _, binding := range s.state.Bindings {
		if binding.TenantID == application.TenantID && binding.PortalID == application.PortalID && binding.ProfileRevisionID == profile.ID {
			return portalapi.ActivationRequest{PortalID: application.PortalID, ApplicationRevisionID: application.ID, ProfileRevisionID: profile.ID, BindingRevisionID: binding.ID, ExpectedCurrentID: expected}
		}
	}
	return portalapi.ActivationRequest{}
}

type configuredHost struct {
	state *stateOnlyHost
	calls []string
}

var _ sdk.Host = (*configuredHost)(nil)

func (h *configuredHost) Call(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if target.GetExtensionPoint() != extpoint.KernelService {
		return nil, nil, errors.New("unexpected extension point")
	}
	h.calls = append(h.calls, target.GetCapability())
	switch target.GetCapability() {
	case "kernel.config.get":
		var request struct {
			Key      string `json:"key"`
			Optional bool   `json:"optional"`
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, nil, errors.New("unexpected state configuration request")
		}
		var value string
		switch request.Key {
		case PlatformCatalogConfigKey:
			raw, _ := json.Marshal(testPlatformCatalog())
			value = string(raw)
		case VersionControlConfigKey:
			if !request.Optional {
				return nil, nil, errors.New("version control lookup must be optional")
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte("null"), nil
		default:
			return nil, nil, errors.New("unexpected configuration key")
		}
		raw, _ := json.Marshal(value)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	case portalapi.KernelCatalogValidationCapability:
		var request struct {
			TenantID string               `json:"tenantId"`
			Spec     portalapi.PortalSpec `json:"spec"`
		}
		if err := json.Unmarshal(payload, &request); err != nil || request.TenantID != "tenant-a" || request.Spec.ID != "admin" {
			return nil, nil, errors.New("unexpected catalog request")
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`{"valid":true}`), nil
	default:
		if strings.HasPrefix(target.GetCapability(), "kernel.state.shared.") {
			return h.state.Call(ctx, target, call, payload)
		}
		return nil, nil, errors.New("unexpected host capability")
	}
}

func TestContributionGetsStateAndCatalogOnlyFromAuthenticatedHost(t *testing.T) {
	service := New(nil)
	host := &configuredHost{state: newStateOnlyHost(t)}
	callCtx := &contractv1.CallContext{
		TenantId:  "tenant-a",
		Principal: &contractv1.Principal{UserId: "author", SystemRoles: []string{"portal.compose"}},
	}
	payload, _ := json.Marshal(portalapi.CreatePortalRequest{PortalID: "admin", Configuration: portalapi.PortalConfiguration{Application: spec("/")}})
	handler := Contribution(service).Handlers["createPortal"]
	result, raw, err := handler(context.Background(), host, callCtx, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("通过可信宿主创建草稿失败: result=%+v err=%v", result, err)
	}
	if len(host.calls) != 6 || host.calls[0] != "kernel.config.get" || host.calls[1] != "kernel.config.get" || host.calls[2] != "kernel.state.shared.get" || host.calls[3] != portalapi.KernelCatalogValidationCapability || host.calls[4] != "kernel.state.shared.create" || host.calls[5] != "kernel.state.shared.create" {
		t.Fatalf("宿主调用路径错误: %v", host.calls)
	}
	var portal portalapi.Portal
	if err := json.Unmarshal(raw, &portal); err != nil || portal.ID != "admin" || portal.WorkingCopy == nil || portal.WorkingCopy.Revision != 1 {
		t.Fatalf("创建草稿响应错误: %s %v", raw, err)
	}
}
