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
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
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
	audit, err := s.Audit(context.Background(), publisher, draft.ID)
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
	audit, err := s.Audit(context.Background(), author, draft.ID)
	if err != nil || len(audit) != 2 || audit[1].Action != "draft.updated" {
		t.Fatalf("更新草稿审计缺失: %+v %v", audit, err)
	}
	if _, err := s.Submit(context.Background(), author, draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateDraft(context.Background(), author, draft.ID, spec("/forbidden")); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("提交后不得更新草稿: %v", err)
	}
}

func TestProfileDraftCanBeDeletedOnlyByItsTenantBeforeSubmission(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	profile := s.state.Profiles[0].Profile
	profile.ID, profile.Revision = "temporary-profile", 1
	draft, err := s.CreateProfileDraft(context.Background(), author, profile)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := s.DeleteProfileDraft(context.Background(), author, draft.ID)
	if err != nil || deleted.ID != draft.ID {
		t.Fatalf("删除 Profile 草稿失败: revision=%+v err=%v", deleted, err)
	}
	if _, err := s.profileIndexLocked(author.TenantID, draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("已删除草稿仍可被读取: %v", err)
	}
	if event := s.state.Audit[len(s.state.Audit)-1]; event.Action != "profile.draft.deleted" || event.RevisionID != draft.ID || event.ActorID != author.ID {
		t.Fatalf("删除草稿必须保留审计记录: %+v", event)
	}

	second, err := s.CreateProfileDraft(context.Background(), author, profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransitionProfile(context.Background(), author, second.ID, "submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteProfileDraft(context.Background(), author, second.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("已提交 Profile 不得删除: %v", err)
	}
	otherTenant := portalapi.Principal{ID: "other", TenantID: "tenant-b", Roles: []string{"portal.compose"}}
	if _, err := s.DeleteProfileDraft(context.Background(), otherTenant, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨租户删除必须隐藏资源存在性: %v", err)
	}
}

func TestPublishRejectsCrossPortalRouteAndBreakGlassNeedsReason(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")
	publish := func(id string, expected uint64) portalapi.PortalActivation {
		d, e := s.CreateDraft(context.Background(), author, spec("/"))
		if e != nil {
			t.Fatal(e)
		}
		s.state.Revisions[len(s.state.Revisions)-1].PortalID = id
		if _, e = s.Submit(context.Background(), author, d.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = s.Approve(context.Background(), approver, d.ID); e != nil {
			t.Fatal(e)
		}
		published, e := s.Publish(context.Background(), publisher, d.ID, portalapi.PublishRequest{})
		if e != nil {
			t.Fatal(e)
		}
		request := activationRequest(s, published, expected)
		request.PortalID = id
		request.ApplicationRevisionID = published.ID
		for _, binding := range s.state.Bindings {
			if binding.PortalID == id {
				request.BindingRevisionID, request.ProfileRevisionID = binding.ID, binding.ProfileRevisionID
			}
		}
		activation, e := s.Activate(context.Background(), publisher, request)
		if e != nil {
			t.Fatal(e)
		}
		return activation
	}
	first := publish("admin", 0)
	d, err := s.CreateDraft(context.Background(), author, spec("/"))
	if err != nil {
		t.Fatal(err)
	}
	s.state.Revisions[len(s.state.Revisions)-1].PortalID = "two"
	if _, err = s.Publish(context.Background(), portalapi.Principal{ID: "system", TenantID: "tenant-a", System: true}, d.ID, portalapi.PublishRequest{}); err == nil {
		t.Fatal("break-glass 缺原因必须拒绝")
	}
	if _, err = s.Submit(context.Background(), author, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Approve(context.Background(), approver, d.ID); err != nil {
		t.Fatal(err)
	}
	published, err := s.Publish(context.Background(), publisher, d.ID, portalapi.PublishRequest{})
	if err != nil {
		t.Fatal(err)
	}
	request := activationRequest(s, published, 0)
	request.PortalID = "two"
	request.ExpectedCurrentID = 0
	request.ApplicationRevisionID = published.ID
	// The fixture catalog has one binding; clone it as a published binding for the second Portal.
	binding := s.state.Bindings[0]
	s.state.NextGovernance++
	binding.ID, binding.PortalID, binding.Binding.PortalID = s.state.NextGovernance, "two", "two"
	s.state.Bindings = append(s.state.Bindings, binding)
	request.BindingRevisionID, request.ProfileRevisionID = binding.ID, binding.ProfileRevisionID
	failed, err := s.Activate(context.Background(), publisher, request)
	if err != nil || failed.Status != portalapi.ActivationFailed || first.Status != portalapi.ActivationCurrent {
		t.Fatalf("同租户跨 Portal 路由冲突必须产生持久失败 Activation: %+v %v", failed, err)
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

func TestProfileAndBindingPublishingDoesNotGoLiveBeforeActivationCAS(t *testing.T) {
	s := newTestService(t)
	author := principal("author", "portal.compose")
	approver := principal("approver", "portal.approve")
	publisher := principal("publisher", "portal.publish")

	baseProfile := s.state.Profiles[0].Profile
	profile := baseProfile
	profile.ID, profile.Revision = "portal-top", 1
	profile.Shell.Config.DefaultTemplate = "top-navigation"
	profileDraft, err := s.CreateProfileDraft(context.Background(), author, profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.TransitionProfile(context.Background(), author, profileDraft.ID, "submit"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TransitionProfile(context.Background(), approver, profileDraft.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	publishedProfile, err := s.TransitionProfile(context.Background(), publisher, profileDraft.ID, "publish")
	if err != nil {
		t.Fatal(err)
	}

	binding := s.state.Bindings[0].Binding
	binding.PlatformProfile = compositioncommonv1.Ref{}
	bindingDraft, err := s.CreateBindingDraft(context.Background(), author, portalapi.BindingDraftRequest{ProfileRevisionID: publishedProfile.ID, Binding: binding})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.TransitionBinding(context.Background(), author, bindingDraft.ID, "submit"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TransitionBinding(context.Background(), approver, bindingDraft.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	publishedBinding, err := s.TransitionBinding(context.Background(), publisher, bindingDraft.ID, "publish")
	if err != nil {
		t.Fatal(err)
	}

	applicationDraft, err := s.CreateDraft(context.Background(), author, spec("/"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Submit(context.Background(), author, applicationDraft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Approve(context.Background(), approver, applicationDraft.ID); err != nil {
		t.Fatal(err)
	}
	publishedApplication, err := s.Publish(context.Background(), publisher, applicationDraft.ID, portalapi.PublishRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListActivations(context.Background(), publisher); err != nil || len(got) != 0 {
		t.Fatalf("Published 输入不得自动上线: %+v %v", got, err)
	}

	request := portalapi.ActivationRequest{PortalID: publishedApplication.PortalID, ApplicationRevisionID: publishedApplication.ID, ProfileRevisionID: publishedProfile.ID, BindingRevisionID: publishedBinding.ID, ExpectedCurrentID: 0, Reason: "切换到顶部导航"}
	current, err := s.Activate(context.Background(), publisher, request)
	if err != nil || current.Status != portalapi.ActivationCurrent || current.Spec.Shell.ID != profile.Shell.ID || current.Spec.Shell.Config.DefaultTemplate != "top-navigation" {
		t.Fatalf("Activation 未使用精确发布输入: %+v %v", current, err)
	}
	if _, err := s.Activate(context.Background(), publisher, request); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("过期 expectedCurrentId 必须被 CAS 拒绝: %v", err)
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
		var request map[string]string
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, nil, errors.New("unexpected state configuration request")
		}
		var value string
		switch request["key"] {
		case PlatformCatalogConfigKey:
			raw, _ := json.Marshal(testPlatformCatalog())
			value = string(raw)
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
	payload, _ := json.Marshal(spec("/"))
	handler := Contribution(service).Handlers["createDraft"]
	result, raw, err := handler(context.Background(), host, callCtx, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("通过可信宿主创建草稿失败: result=%+v err=%v", result, err)
	}
	if len(host.calls) != 5 || host.calls[0] != "kernel.config.get" || host.calls[1] != "kernel.state.shared.get" || host.calls[2] != "kernel.state.shared.create" || host.calls[3] != portalapi.KernelCatalogValidationCapability || host.calls[4] != "kernel.state.shared.update" {
		t.Fatalf("宿主调用路径错误: %v", host.calls)
	}
	var revision portalapi.Revision
	if err := json.Unmarshal(raw, &revision); err != nil || revision.ID != 1 {
		t.Fatalf("创建草稿响应错误: %s %v", raw, err)
	}
}
