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
	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactreference"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.configuration.portal-composer/portalapproval"
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
	catalogConfigured := s.catalogConfigured
	versionControlConfigured := s.versionControlConfigLoaded
	s.mu.Unlock()
	if catalogConfigured && versionControlConfigured {
		return nil
	}
	if !catalogConfigured {
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
		catalog, err = compileBrowserExposure(ctx, host, callCtx, catalog)
		if err != nil {
			return err
		}
		if err := s.BindPlatformCatalog(catalog); err != nil {
			return err
		}
	}
	if versionControlConfigured {
		return nil
	}
	versionRaw, found, err := readOptionalConfig(ctx, host, callCtx, VersionControlConfigKey)
	if err != nil {
		return err
	}
	if !found {
		return s.BindVersionControl(nil)
	}
	binding, err := parseVersionControlBinding(versionRaw)
	if err != nil {
		return err
	}
	return s.BindVersionControl(&binding)
}

func compileBrowserExposure(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, catalog frontendcompositionv1.PortalPlatformCatalog) (frontendcompositionv1.PortalPlatformCatalog, error) {
	if host == nil || callCtx == nil {
		return frontendcompositionv1.PortalPlatformCatalog{}, fmt.Errorf("Portal 浏览器暴露编译调用上下文不完整")
	}
	payload, err := json.Marshal(struct {
		Catalog frontendcompositionv1.PortalPlatformCatalog `json:"catalog"`
	}{Catalog: catalog})
	if err != nil {
		return frontendcompositionv1.PortalPlatformCatalog{}, err
	}
	op := "compile"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.KernelService, Capability: portalapi.KernelCatalogBrowserExposureCompilationCapability, Operation: &op,
	}, callCtx, payload)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return frontendcompositionv1.PortalPlatformCatalog{}, fmt.Errorf("调用可信 Portal 浏览器暴露编译: %w", catalogCallError(result, err))
	}
	compiled, err := frontendcompositionv1.ParsePortalPlatformCatalog(raw)
	if err != nil {
		return frontendcompositionv1.PortalPlatformCatalog{}, fmt.Errorf("可信 Portal 浏览器暴露编译返回无效 Catalog: %w", err)
	}
	return frontendcompositionv1.ValidateResolvedPortalPlatformCatalog(compiled)
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

func readOptionalConfig(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, key string) ([]byte, bool, error) {
	op := "get"
	payload, _ := json.Marshal(struct {
		Key      string `json:"key"`
		Optional bool   `json:"optional"`
	}{Key: key, Optional: true})
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: "kernel.config.get", Operation: &op}, callCtx, payload)
	if err != nil {
		return nil, false, fmt.Errorf("读取 Portal Composer 可选部署配置 %s: %w", key, err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return nil, false, fmt.Errorf("读取 Portal Composer 可选部署配置 %s 失败", key)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false, nil
	}
	return raw, true, nil
}

func parseVersionControlBinding(raw []byte) (PortalVersionControlBinding, error) {
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		raw = []byte(encoded)
	}
	var binding PortalVersionControlBinding
	if err := decodeComposerJSON(raw, &binding); err != nil {
		return PortalVersionControlBinding{}, fmt.Errorf("解析 %s: %w", VersionControlConfigKey, err)
	}
	binding.EnvironmentID = strings.TrimSpace(binding.EnvironmentID)
	binding.ResourceType = strings.TrimSpace(binding.ResourceType)
	if err := binding.validate(); err != nil {
		return PortalVersionControlBinding{}, err
	}
	return binding, nil
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
	for _, operation := range signedToolOperationNames(Capability) {
		op := operation
		handlers[op] = func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if err := service.ensureConfigured(ctx, host, callCtx); err != nil {
				return nil, nil, err
			}
			principal, err := projectPrincipalForOperation(callCtx, op)
			if err != nil {
				return nil, nil, err
			}
			var raw []byte
			versionControl, err := newWorkspacePortalVersionControl(host, callCtx)
			if err != nil {
				return nil, nil, err
			}
			err = service.withTenantState(ctx, host, callCtx, principal.TenantID, func() error {
				var handleErr error
				requestContext := withCatalog(ctx, hostCatalog{host: host, callCtx: callCtx})
				requestContext = withVersionControl(requestContext, versionControl)
				if portalapproval.OperationNeedsPolicy(op) {
					policy, policyErr := portalapproval.NewHostPolicy(host, callCtx, service.approvalBinding)
					if policyErr != nil {
						return policyErr
					}
					requestContext = portalapproval.WithPolicy(requestContext, policy)
				}
				raw, handleErr = service.Handle(requestContext, principal, op, payload)
				return handleErr
			})
			if err != nil {
				if approval, ok := portalapproval.AsError(err); ok {
					return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "portal." + approval.Decision.Code, Message: approval.Error()}}, nil, nil
				}
				if errors.Is(err, ErrForbidden) {
					return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: errorcode.PermissionDenied, Message: err.Error()}}, nil, nil
				}
				if result := navigationComposerOperationError(op, err); result != nil {
					return result, nil, nil
				}
				if errors.Is(err, ErrStateConflict) {
					return composerStateError("portal.composer.conflict", err, true), nil, nil
				}
				if errors.Is(err, portalapproval.ErrProviderUnavailable) {
					return composerStateError("portal.approval.provider_unavailable", err, true), nil, nil
				}
				if errors.Is(err, ErrVersionControlUnavailable) {
					return composerStateError("portal.version_control_unavailable", err, true), nil, nil
				}
				if errors.Is(err, ErrCatalogRejected) {
					return composerStateError("portal.catalog.rejected", ErrCatalogRejected, false), nil, nil
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

func navigationComposerOperationError(operation string, err error) *contractv1.CallResult {
	switch operation {
	case portalapi.ReadNavigationConfigurationOperation, portalapi.PrepareNavigationConfigurationOperation, portalapi.CommitNavigationConfigurationOperation, portalapi.AbortNavigationConfigurationOperation, portalapi.RollbackNavigationConfigurationOperation:
		if errors.Is(err, ErrInvalidState) || errors.Is(err, ErrNotFound) {
			return composerStateError("portal.composer.conflict", err, true)
		}
	}
	return nil
}

func Descriptor() []byte {
	return signedToolDescriptor(Capability)
}
func projectPrincipal(callCtx *contractv1.CallContext) (portalapi.Principal, error) {
	if callCtx == nil || callCtx.Principal == nil || callCtx.Principal.UserId == "" || callCtx.TenantId == "" {
		return portalapi.Principal{}, fmt.Errorf("Portal capability 必须携带经验证的 Principal 和 tenant")
	}
	roles := append([]string(nil), callCtx.Principal.SystemRoles...)
	return portalapi.Principal{ID: callCtx.Principal.UserId, TenantID: callCtx.TenantId, Roles: roles, System: callCtx.Principal.UserId == "system"}, nil
}

func projectPrincipalForOperation(callCtx *contractv1.CallContext, operation string) (portalapi.Principal, error) {
	switch operation {
	case portalapi.ExecutePublicationReleaseOperation:
		if callCtx != nil && callCtx.GetTenantId() != "" && callCtx.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_PLUGIN && callCtx.GetCaller().GetId() == workflowv1.OrchestratorPluginID {
			return portalapi.Principal{ID: workflowv1.OrchestratorPluginID, TenantID: callCtx.GetTenantId(), System: true}, nil
		}
		return portalapi.Principal{}, ErrForbidden
	case portalapi.PreparePluginInstallationOperation, portalapi.CommitPluginInstallationOperation, portalapi.AbortPluginInstallationOperation, portalapi.RollbackPluginInstallationOperation:
		if callCtx != nil && callCtx.GetTenantId() != "" && callCtx.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_PLUGIN && callCtx.GetCaller().GetId() == "cn.vastplan.platform.infrastructure.deployment-manager" {
			return portalapi.Principal{ID: "system", TenantID: callCtx.GetTenantId(), System: true}, nil
		}
		return portalapi.Principal{}, ErrForbidden
	case portalapi.ReadNavigationConfigurationOperation, portalapi.PrepareNavigationConfigurationOperation, portalapi.CommitNavigationConfigurationOperation, portalapi.AbortNavigationConfigurationOperation, portalapi.RollbackNavigationConfigurationOperation:
		if callCtx != nil && callCtx.GetTenantId() != "" && callCtx.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_PLUGIN && callCtx.GetCaller().GetId() == portalapi.NavigationOrganizerPluginID && callCtx.GetPrincipal().GetUserId() != "" {
			return portalapi.Principal{ID: callCtx.GetPrincipal().GetUserId(), TenantID: callCtx.GetTenantId(), Roles: append([]string(nil), callCtx.GetPrincipal().GetSystemRoles()...), System: true}, nil
		}
		return portalapi.Principal{}, ErrForbidden
	default:
		return projectPrincipal(callCtx)
	}
}
