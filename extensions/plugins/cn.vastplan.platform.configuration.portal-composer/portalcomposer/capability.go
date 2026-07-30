package portalcomposer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/errorcode"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactreference"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

type catalogContextKey struct{}

func withCatalog(ctx context.Context, catalog Catalog) context.Context {
	return context.WithValue(ctx, catalogContextKey{}, catalog)
}

func (s *Service) validateCatalog(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	catalog, _ := ctx.Value(catalogContextKey{}).(Catalog)
	if catalog == nil {
		catalog = s.artifactCatalog
	}
	if catalog == nil {
		return fmt.Errorf("Portal Composer 未获得受信任制品目录")
	}
	return catalog.ValidatePortal(ctx, tenantID, spec)
}

func (s *Service) materializeCatalog(ctx context.Context, tenantID string, spec portalapi.PortalSpec) ([]pluginv1.ArtifactReference, error) {
	catalog, _ := ctx.Value(catalogContextKey{}).(Catalog)
	if catalog == nil {
		catalog = s.artifactCatalog
	}
	if catalog == nil {
		return nil, fmt.Errorf("Portal Composer 未获得受信任制品目录")
	}
	return catalog.MaterializePortal(ctx, tenantID, spec)
}

func (s *Service) publishReferenceSnapshot(ctx context.Context, value pluginv1.ArtifactReferenceSnapshot) error {
	catalog, _ := ctx.Value(catalogContextKey{}).(Catalog)
	if catalog == nil {
		catalog = s.artifactCatalog
	}
	if catalog == nil {
		return fmt.Errorf("Portal Composer 未获得受信任制品引用发布器")
	}
	sealed, err := artifactreference.Seal(value)
	if err != nil {
		return err
	}
	return catalog.PublishReferenceSnapshot(ctx, sealed)
}

func (s *Service) validateTestArtifact(ctx context.Context, tenantID string, request portalapi.CreateTestReleaseRequest, publishers []string) error {
	catalog, _ := ctx.Value(catalogContextKey{}).(Catalog)
	if catalog == nil {
		catalog = s.artifactCatalog
	}
	if catalog == nil {
		return fmt.Errorf("Portal Composer 未获得受信任制品目录")
	}
	testCatalog, ok := catalog.(TestArtifactCatalog)
	if !ok {
		return fmt.Errorf("Portal Composer 未获得受信任测试制品目录")
	}
	return testCatalog.ValidateTestArtifact(ctx, tenantID, request, publishers)
}

// hostCatalog makes the artifact decision in the trusted kernel boundary. The
// plugin can ask whether a spec is valid, but never receives repository
// credentials, verification keys, or an unverified artifact envelope.
type hostCatalog struct {
	host    sdk.Host
	callCtx *contractv1.CallContext
}

func (c hostCatalog) ValidatePortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) error {
	return c.call(ctx, tenantID, spec, portalapi.KernelCatalogValidationCapability, "validate")
}

func (c hostCatalog) MaterializePortal(ctx context.Context, tenantID string, spec portalapi.PortalSpec) ([]pluginv1.ArtifactReference, error) {
	if c.host == nil || c.callCtx == nil || strings.TrimSpace(tenantID) == "" {
		return nil, fmt.Errorf("Portal 制品目录调用上下文不完整")
	}
	payload, err := json.Marshal(struct {
		TenantID string               `json:"tenantId"`
		Spec     portalapi.PortalSpec `json:"spec"`
	}{TenantID: tenantID, Spec: spec})
	if err != nil {
		return nil, err
	}
	op := "materialize"
	result, raw, err := c.host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: portalapi.KernelCatalogMaterializationCapability, Operation: &op}, c.callCtx, payload)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return nil, fmt.Errorf("调用可信 Portal 制品目录 materialize: %w", catalogCallError(result, err))
	}
	var response struct {
		References []pluginv1.ArtifactReference `json:"references"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.References == nil {
		return nil, fmt.Errorf("可信 Portal 制品目录返回的引用无效")
	}
	return response.References, nil
}

func (c hostCatalog) PublishReferenceSnapshot(ctx context.Context, value pluginv1.ArtifactReferenceSnapshot) error {
	if c.host == nil || c.callCtx == nil {
		return fmt.Errorf("Portal 制品引用调用上下文不完整")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	op := "publish"
	result, _, err := c.host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.KernelService, Capability: portalapi.KernelArtifactReferencePublicationCapability, Operation: &op,
	}, c.callCtx, payload)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return fmt.Errorf("提交 Portal 制品引用保护失败: %w", catalogCallError(result, err))
	}
	return nil
}

func coalesceCatalogError(err error) error {
	if err != nil {
		return err
	}
	return errors.New("可信宿主拒绝")
}

func catalogCallError(result *contractv1.CallResult, err error) error {
	if err != nil {
		return err
	}
	if result != nil && result.Error != nil && strings.TrimSpace(result.Error.Message) != "" {
		if strings.TrimSpace(result.Error.Code) != "" {
			return fmt.Errorf("%s: %s", result.Error.Code, result.Error.Message)
		}
		return errors.New(result.Error.Message)
	}
	return coalesceCatalogError(nil)
}

func (c hostCatalog) ValidateTestArtifact(ctx context.Context, tenantID string, request portalapi.CreateTestReleaseRequest, allowedPublishers []string) error {
	if c.host == nil || c.callCtx == nil || strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("Portal 测试制品调用上下文不完整")
	}
	payload, err := json.Marshal(struct {
		TenantID          string                             `json:"tenantId"`
		Request           portalapi.CreateTestReleaseRequest `json:"request"`
		AllowedPublishers []string                           `json:"allowedPublishers"`
	}{TenantID: tenantID, Request: request, AllowedPublishers: allowedPublishers})
	if err != nil {
		return err
	}
	op := "validate"
	result, _, err := c.host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.KernelService, Capability: portalapi.KernelTestArtifactValidationCapability, Operation: &op,
	}, c.callCtx, payload)
	if err != nil {
		return fmt.Errorf("调用可信 Portal 测试制品验证: %w", err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return fmt.Errorf("可信 Portal 测试制品验证拒绝")
	}
	return nil
}

func (c hostCatalog) call(ctx context.Context, tenantID string, spec portalapi.PortalSpec, capability, operation string) error {
	if c.host == nil || c.callCtx == nil || strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("Portal 制品目录调用上下文不完整")
	}
	payload, err := json.Marshal(struct {
		TenantID string               `json:"tenantId"`
		Spec     portalapi.PortalSpec `json:"spec"`
	}{TenantID: tenantID, Spec: spec})
	if err != nil {
		return err
	}
	op := operation
	result, _, err := c.host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.KernelService,
		Capability:     capability,
		Operation:      &op,
	}, c.callCtx, payload)
	if err != nil {
		return fmt.Errorf("调用可信 Portal 制品目录 %s: %w", operation, err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return fmt.Errorf("可信 Portal 制品目录拒绝校验: %w", catalogCallError(result, nil))
	}
	return nil
}

func (s *Service) ensureConfigured(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext) error {
	s.mu.Lock()
	configured := s.catalogConfigured
	s.mu.Unlock()
	if configured {
		return nil
	}
	catalogRaw, err := readConfig(ctx, host, callCtx, PlatformCatalogConfigKey)
	if err != nil {
		return err
	}
	var encodedCatalog string
	if err := json.Unmarshal(catalogRaw, &encodedCatalog); err != nil || strings.TrimSpace(encodedCatalog) == "" {
		return fmt.Errorf("%s 必须是非空 JSON 字符串", PlatformCatalogConfigKey)
	}
	catalog, err := frontendcompositionv1.ParsePortalPlatformCatalog([]byte(encodedCatalog))
	if err != nil {
		return err
	}
	return s.BindPlatformCatalog(catalog)
}

func readConfig(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, key string) ([]byte, error) {
	op := "get"
	payload, _ := json.Marshal(map[string]string{"key": key})
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: "kernel.config.get", Operation: &op}, callCtx, payload)
	if err != nil {
		return nil, fmt.Errorf("读取 Portal Composer 部署配置 %s: %w", key, err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return nil, fmt.Errorf("未提供 Portal Composer 部署配置 %s", key)
	}
	return raw, nil
}

// Handle is the wire boundary used by the plugin capability adapter. Principal
// is supplied by the trusted host call context, never decoded from browser JSON.
func (s *Service) Handle(ctx context.Context, principal portalapi.Principal, operation string, payload []byte) ([]byte, error) {
	result, err := s.handlePortalOperation(ctx, principal, operation, payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func decode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("Portal Composer 请求无效: %w", err)
	}
	return nil
}

// Contribution exposes governance through the standard capability bus. The
// Host has already authenticated the caller; this adapter only projects the
// minimum fields required by the portal API.
func Contribution(service *Service) sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range []string{"createDraft", "updateDraft", "list", "submit", "approve", "publish", "audit", "governance", "createProfileDraft", "updateProfileDraft", "deleteProfileDraft", "transitionProfile", "createBindingDraft", "updateBindingDraft", "transitionBinding", "activate", "rollbackActivation", "listActivations", "listTestTargetBindings", "putTestTargetBinding", "listTestReleases", "createTestRelease", "rollbackTestRelease"} {
		op := operation
		handlers[op] = func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if err := service.ensureConfigured(ctx, host, callCtx); err != nil {
				return nil, nil, err
			}
			principal, err := projectPrincipal(callCtx)
			if err != nil {
				return nil, nil, err
			}
			var raw []byte
			err = service.withTenantState(ctx, host, callCtx, principal.TenantID, func() error {
				var handleErr error
				raw, handleErr = service.Handle(withCatalog(ctx, hostCatalog{host: host, callCtx: callCtx}), principal, op, payload)
				return handleErr
			})
			if err != nil {
				if errors.Is(err, ErrForbidden) || errors.Is(err, ErrSelfApproval) {
					return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: errorcode.PermissionDenied, Message: err.Error()}}, nil, nil
				}
				if errors.Is(err, ErrStateConflict) {
					return composerStateError("portal.composer.conflict", err, true), nil, nil
				}
				var stateError *sharedstatesdk.ServiceError
				if errors.As(err, &stateError) {
					return composerStateError("portal.composer.unavailable", err, stateError.Retryable), nil, nil
				}
				return nil, nil, err
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}

func composerStateError(code string, err error, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error(), Retryable: retryable}}
}

func Descriptor() []byte {
	return []byte(`{"title":"门户组合治理","subcommands":[{"name":"governance","description":"读取完整 Portal 治理工作区"},{"name":"createDraft","description":"创建 Application 草稿"},{"name":"updateDraft","description":"更新 Application 草稿"},{"name":"list","description":"列出 Application revisions"},{"name":"submit","description":"提交 Application 审批"},{"name":"approve","description":"审批 Application"},{"name":"publish","description":"发布 Application 输入"},{"name":"createProfileDraft","description":"创建 Profile 草稿"},{"name":"updateProfileDraft","description":"更新 Profile 草稿"},{"name":"deleteProfileDraft","description":"删除 Profile 草稿"},{"name":"transitionProfile","description":"推进 Profile 生命周期"},{"name":"createBindingDraft","description":"创建 Binding 草稿"},{"name":"updateBindingDraft","description":"更新 Binding 草稿"},{"name":"transitionBinding","description":"推进 Binding 生命周期"},{"name":"activate","description":"CAS 激活 Published 输入"},{"name":"rollbackActivation","description":"由历史 Activation 创建新激活"},{"name":"listActivations","description":"列出不可变 Activation"},{"name":"listTestTargetBindings","description":"列出 Frontend 测试目标绑定"},{"name":"putTestTargetBinding","description":"CAS 保存 Frontend 应用插件测试目标"},{"name":"listTestReleases","description":"列出 Frontend Test Release"},{"name":"createTestRelease","description":"提交精确 Frontend 测试制品"},{"name":"rollbackTestRelease","description":"恢复中断的 Frontend Test Release"},{"name":"audit","description":"读取 revision 审计"}]}`)
}
func projectPrincipal(callCtx *contractv1.CallContext) (portalapi.Principal, error) {
	if callCtx == nil || callCtx.Principal == nil || callCtx.Principal.UserId == "" || callCtx.TenantId == "" {
		return portalapi.Principal{}, fmt.Errorf("Portal capability 必须携带经验证的 Principal 和 tenant")
	}
	roles := append([]string(nil), callCtx.Principal.SystemRoles...)
	return portalapi.Principal{ID: callCtx.Principal.UserId, TenantID: callCtx.TenantId, Roles: roles, System: callCtx.Principal.UserId == "system"}, nil
}
