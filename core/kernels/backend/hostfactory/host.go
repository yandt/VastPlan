// Package hostfactory 集中声明 backend 内核的扩展点和内置能力。
//
// 手工演示入口与 Node Agent 自动装配必须使用同一宿主工厂；否则两条启动路径会
// 悄悄形成不同的内核能力集合。
package hostfactory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/operationfence"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
)

// KernelName 是 backend 内核规范 ID。
const KernelName = "backend"

// New 创建尚未 Start 的 backend 插件宿主。
func New(version string, logf func(string, ...any)) (*protocolbus.Host, error) {
	return NewWithDependencies(version, logf, kernelspi.Dependencies{})
}

func NewWithDependencies(version string, logf func(string, ...any), dependencies kernelspi.Dependencies) (*protocolbus.Host, error) {
	reg := registry.New()
	for _, point := range []registry.ExtensionPoint{
		{Name: extpoint.ToolPackage, Dispatch: registry.DispatchSingle},
		{Name: extpoint.Agent, Dispatch: registry.DispatchSingle},
		{Name: extpoint.APIRoute, Dispatch: registry.DispatchSingle},
		{Name: extpoint.AuthenticationProvider, Dispatch: registry.DispatchSingle},
		{Name: extpoint.ConfigurationController, Dispatch: registry.DispatchSingle},
		{Name: extpoint.ConfigurationResourceController, Dispatch: registry.DispatchSingle},
		{Name: extpoint.ConfigurationScopedResolver, Dispatch: registry.DispatchSingle},
		{Name: extpoint.PermissionChecker, Dispatch: registry.DispatchSelect},
		{Name: extpoint.EventSink, Dispatch: registry.DispatchFanout},
		{Name: extpoint.Hook, Dispatch: registry.DispatchFanout},
		{Name: extpoint.RunnerCapability, Dispatch: registry.DispatchSingle},
		{Name: extpoint.KernelService, Dispatch: registry.DispatchSingle},
	} {
		reg.DefinePoint(point)
	}
	host := protocolbus.NewHost(KernelName, version, reg, logf)
	if err := host.RegisterHostService(extpoint.KernelService, "kernel.info", kernelInfo(version)); err != nil {
		return nil, err
	}
	if err := host.RegisterHostService(extpoint.KernelService, "kernel.diagnostics", kernelDiagnostics(host)); err != nil {
		return nil, err
	}
	if dependencies.Config != nil {
		if err := host.RegisterHostService(extpoint.KernelService, "kernel.config.get", kernelConfigGet(dependencies.Config)); err != nil {
			return nil, err
		}
	}
	if dependencies.ManagedCredentialRefs != nil {
		if err := host.RegisterHostService(extpoint.KernelService, pluginconfig.KernelCredentialRefService, kernelManagedCredentialRef(dependencies.ManagedCredentialRefs)); err != nil {
			return nil, err
		}
	}
	if dependencies.RuntimeMaterialLeases != nil {
		if err := host.RegisterHostService(extpoint.KernelService, credentiallease.RuntimeKernelService, kernelRuntimeMaterialLease(dependencies.RuntimeMaterialLeases)); err != nil {
			return nil, err
		}
	}
	if dependencies.NodeBootstrap != nil {
		if err := host.RegisterHostService(extpoint.KernelService, nodebootstrap.KernelService, kernelNodeBootstrap(dependencies.NodeBootstrap)); err != nil {
			return nil, err
		}
	}
	if dependencies.NodeReadiness != nil {
		if err := host.RegisterHostService(extpoint.KernelService, nodebootstrap.KernelReadinessService, kernelNodeReadiness(dependencies.NodeReadiness)); err != nil {
			return nil, err
		}
	}
	if dependencies.DeploymentPublication != nil {
		if err := host.RegisterHostService(extpoint.KernelService, deploymentpublication.KernelTargetsService, kernelDeploymentTargets(dependencies.DeploymentPublication)); err != nil {
			return nil, err
		}
		if err := host.RegisterHostService(extpoint.KernelService, deploymentpublication.KernelPreviewService, kernelDeploymentPreview(dependencies.DeploymentPublication)); err != nil {
			return nil, err
		}
		if err := host.RegisterHostService(extpoint.KernelService, deploymentpublication.KernelPublishService, kernelDeploymentPublish(dependencies.DeploymentPublication)); err != nil {
			return nil, err
		}
	}
	if dependencies.DeploymentReadiness != nil {
		if err := host.RegisterHostService(extpoint.KernelService, deploymentpublication.KernelReadinessService, kernelDeploymentReadiness(dependencies.DeploymentReadiness)); err != nil {
			return nil, err
		}
	}
	if dependencies.PlatformProfileActivation != nil {
		services := kernelPlatformProfileActivation(dependencies.PlatformProfileActivation)
		names := make([]string, 0, len(services))
		for name := range services {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			service := services[name]
			if err := host.RegisterHostService(extpoint.KernelService, name, service); err != nil {
				return nil, err
			}
		}
	}
	if dependencies.ConfigurationCatalogs != nil {
		if err := host.RegisterHostService(extpoint.KernelService, pluginconfiguration.KernelCatalogsService, kernelConfigurationCatalogs(dependencies.ConfigurationCatalogs)); err != nil {
			return nil, err
		}
	}
	if dependencies.ConfigurationAuthorityIssuer != nil {
		if err := host.RegisterHostService(extpoint.KernelService, configurationauthority.KernelIssueService, kernelConfigurationAuthorityIssue(dependencies.ConfigurationAuthorityIssuer)); err != nil {
			return nil, err
		}
	}
	if dependencies.ConfigurationAuthorityConsumer != nil {
		if err := host.RegisterHostService(extpoint.KernelService, configurationauthority.KernelConsumeService, kernelConfigurationAuthorityConsume(dependencies.ConfigurationAuthorityConsumer)); err != nil {
			return nil, err
		}
	}
	if dependencies.SharedState != nil {
		for _, operation := range []string{
			sharedstatev1.OperationGet, sharedstatev1.OperationCreate, sharedstatev1.OperationUpdate,
			sharedstatev1.OperationDelete, sharedstatev1.OperationList,
		} {
			if err := host.RegisterHostService(extpoint.KernelService, sharedstatev1.KernelService(operation), kernelSharedState(dependencies.SharedState, operation)); err != nil {
				return nil, err
			}
		}
	}
	return host, nil
}

func kernelSharedState(store sharedstate.Store, operation string) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		identity, ok := runtimeidentity.FromContext(ctx)
		if !ok || identity.Validate() != nil || callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != identity.PluginID {
			return sharedStateError("state.identity_invalid", "Shared State 缺少可信 Runtime 身份", false), nil, nil
		}
		request, err := sharedstatev1.ParseRequest(operation, payload)
		if err != nil {
			return sharedStateError("state.invalid", "Shared State 请求无效", false), nil, nil
		}
		scopeName, namespace := sharedStateRequestScope(request)
		scope := sharedstate.Scope{Kind: sharedstate.ScopeKind(scopeName), PluginID: identity.PluginID, RuntimeScope: identity.RuntimeScope, Namespace: namespace}
		if scope.Kind == sharedstate.ScopeTenant {
			scope.TenantID = callCtx.GetTenantId()
		}
		if err := scope.Validate(); err != nil {
			return sharedStateError("state.scope_invalid", "Shared State scope 无效", false), nil, nil
		}
		var response any
		switch typed := request.(type) {
		case *sharedstatev1.KeyRequest:
			response, err = store.Get(ctx, scope, typed.Key)
		case *sharedstatev1.WriteRequest:
			var value []byte
			value, err = sharedstatev1.DecodeValue(typed.Value)
			if err == nil && operation == sharedstatev1.OperationCreate {
				response, err = store.Create(ctx, scope, typed.Key, value)
			} else if err == nil {
				response, err = store.Update(ctx, scope, typed.Key, value, typed.ExpectedRevision)
			}
		case *sharedstatev1.DeleteRequest:
			err = store.Delete(ctx, scope, typed.Key, typed.ExpectedRevision)
			response = map[string]any{"protocol": sharedstatev1.Protocol}
		case *sharedstatev1.ListRequest:
			response, err = store.List(ctx, scope, typed.Prefix, typed.Limit, typed.PageCursor)
		default:
			err = sharedstate.ErrInvalid
		}
		if err != nil {
			return sharedStateStoreError(err), nil, nil
		}
		raw, err := marshalSharedStateResponse(response)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

func sharedStateRequestScope(request any) (string, string) {
	switch typed := request.(type) {
	case *sharedstatev1.KeyRequest:
		return typed.Scope, typed.Namespace
	case *sharedstatev1.WriteRequest:
		return typed.Scope, typed.Namespace
	case *sharedstatev1.DeleteRequest:
		return typed.Scope, typed.Namespace
	case *sharedstatev1.ListRequest:
		return typed.Scope, typed.Namespace
	default:
		return "", ""
	}
}

func marshalSharedStateResponse(value any) ([]byte, error) {
	switch typed := value.(type) {
	case sharedstate.Entry:
		return json.Marshal(sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: typed.Key, Value: sharedstatev1.EncodeValue(typed.Value), Revision: typed.Revision, UpdatedAt: typed.UpdatedAt})
	case sharedstate.Page:
		items := make([]sharedstatev1.Entry, 0, len(typed.Items))
		for _, item := range typed.Items {
			items = append(items, sharedstatev1.Entry{Protocol: sharedstatev1.Protocol, Key: item.Key, Value: sharedstatev1.EncodeValue(item.Value), Revision: item.Revision, UpdatedAt: item.UpdatedAt})
		}
		return json.Marshal(sharedstatev1.Page{Protocol: sharedstatev1.Protocol, Items: items, NextPageCursor: typed.NextCursor})
	default:
		return json.Marshal(value)
	}
}

func sharedStateStoreError(err error) *contractv1.CallResult {
	switch {
	case errors.Is(err, sharedstate.ErrNotFound):
		return sharedStateError("state.not_found", "Shared State 条目不存在", false)
	case errors.Is(err, sharedstate.ErrConflict):
		return sharedStateError("state.conflict", "Shared State revision 冲突", true)
	case errors.Is(err, sharedstate.ErrInvalid):
		return sharedStateError("state.invalid", "Shared State 请求无效", false)
	default:
		return sharedStateError("state.unavailable", "Shared State Provider 不可用", true)
	}
}

func sharedStateError(code, message string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: message, Retryable: retryable}}
}

func kernelConfigurationAuthorityIssue(issuer configurationauthority.Issuer) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != configurationauthority.CoordinatorPluginID || callCtx.GetTenantId() == "" {
			return nil, nil, errors.New("配置授权签发只接受 plugin-settings 认证会话")
		}
		var request configurationauthority.IssueRequest
		if err := decodeStrict(payload, &request); err != nil {
			return nil, nil, configurationauthority.ErrInvalid
		}
		issued, err := issuer.Issue(ctx, callCtx.GetTenantId(), request)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(issued)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelConfigurationAuthorityConsume(consumer configurationauthority.Consumer) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != configurationauthority.CustodianPluginID || callCtx.GetTenantId() == "" {
			return nil, nil, errors.New("配置授权消费只接受 credentials 认证会话")
		}
		var request struct {
			Token string `json:"token"`
		}
		if err := decodeStrict(payload, &request); err != nil {
			return nil, nil, configurationauthority.ErrInvalid
		}
		claims, err := consumer.Consume(ctx, callCtx.GetTenantId(), request.Token)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(claims)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelConfigurationCatalogs(reader pluginconfiguration.Reader) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != pluginconfiguration.PluginSettingsID || callCtx.GetTenantId() == "" {
			return nil, nil, errors.New("kernel.configuration.catalogs 只接受 plugin-settings 认证会话")
		}
		if err := decodeStrict(payload, &struct{}{}); err != nil {
			return nil, nil, errors.New("配置目录请求无效")
		}
		items, err := reader.List(ctx, callCtx.GetTenantId())
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(map[string]any{"items": items})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelRuntimeMaterialLease(broker kernelspi.RuntimeMaterialLeaseBroker) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		identity, ok := runtimeidentity.FromContext(ctx)
		if !ok || callCtx.GetTenantId() == "" || callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN ||
			callCtx.GetCaller().GetId() != identity.PluginID {
			return nil, nil, errors.New("kernel.credential.material-lease 缺少可信 Runtime 启动身份")
		}
		var request credentiallease.Request
		if err := decodeStrict(payload, &request); err != nil {
			return nil, nil, errors.New("runtime material lease 请求无效")
		}
		envelope, err := broker.IssueRuntimeLease(ctx, callCtx.GetTenantId(), identity, request)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(envelope)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

func authenticatedDeploymentManager(callCtx *contractv1.CallContext) bool {
	return callCtx.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_PLUGIN && callCtx.GetCaller().GetId() == deploymentpublication.DeploymentManagerPluginID && callCtx.GetTenantId() != ""
}

func deploymentManagerFence(ctx context.Context, callCtx *contractv1.CallContext, operationID string) (operationfence.Fence, error) {
	if !authenticatedDeploymentManager(callCtx) {
		return operationfence.Fence{}, errors.New("Deployment Manager execution fence 身份无效")
	}
	identity, ok := runtimeidentity.FromContext(ctx)
	if !ok || identity.Validate() != nil || identity.PluginID != deploymentpublication.DeploymentManagerPluginID {
		return operationfence.Fence{}, errors.New("Deployment Manager execution fence 缺少可信 Runtime 身份")
	}
	evidence, ok := operationfence.FromContext(ctx)
	if !ok || evidence.LogicalService != "platform.deployment" || evidence.UnitID != identity.RuntimeScope {
		return operationfence.Fence{}, errors.New("Deployment Manager 已失去当前 leader execution fence")
	}
	fence, err := evidence.ForOperation(operationID)
	if err != nil {
		return operationfence.Fence{}, errors.New("Deployment Manager operationId 无效")
	}
	return fence, nil
}

func kernelDeploymentTargets(controller deploymentpublication.Controller) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !authenticatedDeploymentManager(callCtx) {
			return nil, nil, errors.New("kernel.deployment.targets 只接受 deployment-manager 认证会话")
		}
		if err := decodeStrict(payload, &struct{}{}); err != nil {
			return nil, nil, errors.New("部署目标请求无效")
		}
		targets, err := controller.Targets(ctx, callCtx.GetTenantId())
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(map[string]any{"items": targets})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelDeploymentPreview(controller deploymentpublication.Controller) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !authenticatedDeploymentManager(callCtx) {
			return nil, nil, errors.New("kernel.deployment.preview 只接受 deployment-manager 认证会话")
		}
		var request deploymentpublication.PreviewRequest
		if err := decodeStrict(payload, &request); err != nil {
			return nil, nil, errors.New("部署预览请求无效")
		}
		result, err := controller.Preview(ctx, callCtx.GetTenantId(), request.Composition, request.DeploymentRevision)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(result)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelDeploymentPublish(controller deploymentpublication.Controller) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !authenticatedDeploymentManager(callCtx) {
			return nil, nil, errors.New("kernel.deployment.publish 只接受 deployment-manager 认证会话")
		}
		var request deploymentpublication.PublishRequest
		if err := decodeStrict(payload, &request); err != nil {
			return nil, nil, errors.New("部署发布请求无效")
		}
		if _, err := deploymentManagerFence(ctx, callCtx, fmt.Sprintf("deployment/%s/revision/%d", request.Composition.Metadata.Name, request.DeploymentRevision)); err != nil {
			return nil, nil, err
		}
		result, err := controller.Publish(ctx, callCtx.GetTenantId(), request.Composition, request.DeploymentRevision, request.ExpectedDigest)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(result)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelDeploymentReadiness(observer deploymentpublication.ReadinessObserver) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !authenticatedDeploymentManager(callCtx) {
			return nil, nil, errors.New("kernel.deployment.readiness 只接受 deployment-manager 认证会话")
		}
		var request deploymentpublication.ReadinessRequest
		if err := decodeStrict(payload, &request); err != nil || request.DeploymentName == "" || request.DeploymentRevision == 0 {
			return nil, nil, errors.New("部署 readiness 请求无效")
		}
		observation, err := observer.Observe(ctx, callCtx.GetTenantId(), request.DeploymentName, request.DeploymentRevision)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(observation)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelPlatformProfileActivation(controller platformprofileactivation.Controller) map[string]protocolbus.HostService {
	candidate := func(action string, mutating bool, run func(context.Context, string, platformprofileactivation.CandidateRequest) (platformprofileactivation.Candidate, error)) protocolbus.HostService {
		return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if !authenticatedDeploymentManager(callCtx) {
				return nil, nil, errors.New("Platform Profile Activation 只接受 deployment-manager 认证会话")
			}
			var request platformprofileactivation.CandidateRequest
			if err := decodeStrict(payload, &request); err != nil {
				return nil, nil, errors.New("Platform Profile 候选请求无效")
			}
			if mutating {
				if _, err := deploymentManagerFence(ctx, callCtx, "platform-profile/"+request.CandidateID+"/"+action); err != nil {
					return nil, nil, err
				}
			}
			result, err := run(ctx, callCtx.GetTenantId(), request)
			if err != nil {
				return nil, nil, err
			}
			raw, err := json.Marshal(result)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
		}
	}
	return map[string]protocolbus.HostService{
		platformprofileactivation.KernelPrepareService: func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if !authenticatedDeploymentManager(callCtx) {
				return nil, nil, errors.New("Platform Profile Activation 只接受 deployment-manager 认证会话")
			}
			var request platformprofileactivation.PrepareRequest
			if err := decodeStrict(payload, &request); err != nil {
				return nil, nil, errors.New("Platform Profile 候选准备请求无效")
			}
			if _, err := deploymentManagerFence(ctx, callCtx, "platform-profile/"+request.CandidateID+"/prepare"); err != nil {
				return nil, nil, err
			}
			result, err := controller.Prepare(ctx, callCtx.GetTenantId(), request)
			if err != nil {
				return nil, nil, err
			}
			raw, err := json.Marshal(result)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
		},
		platformprofileactivation.KernelStatusService:   candidate("status", false, controller.Status),
		platformprofileactivation.KernelActivateService: candidate("activate", true, controller.Activate),
		platformprofileactivation.KernelFinalizeService: candidate("finalize", true, controller.Finalize),
		platformprofileactivation.KernelAbortService:    candidate("abort", true, controller.Abort),
		platformprofileactivation.KernelRollbackService: candidate("rollback", true, controller.Rollback),
		platformprofileactivation.KernelPublishService: func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if !authenticatedDeploymentManager(callCtx) {
				return nil, nil, errors.New("Platform Profile Activation 只接受 deployment-manager 认证会话")
			}
			var request platformprofileactivation.PublishRequest
			if err := decodeStrict(payload, &request); err != nil {
				return nil, nil, errors.New("Platform Profile 候选发布请求无效")
			}
			if _, err := deploymentManagerFence(ctx, callCtx, "platform-profile/"+request.Prepare.CandidateID+"/publish"); err != nil {
				return nil, nil, err
			}
			result, err := controller.Publish(ctx, callCtx.GetTenantId(), request)
			if err != nil {
				return nil, nil, err
			}
			raw, err := json.Marshal(result)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
		},
	}
}

func decodeStrict(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("请求只能包含一个 JSON 文档")
	}
	return nil
}

func kernelNodeReadiness(observer nodebootstrap.ReadinessObserver) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != nodebootstrap.DeploymentManagerPluginID {
			return nil, nil, errors.New("kernel.node.readiness 只接受 deployment-manager 认证会话")
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		var expectation nodebootstrap.ReadinessExpectation
		if err := decoder.Decode(&expectation); err != nil {
			return nil, nil, errors.New("节点就绪期望无效")
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, nil, errors.New("节点就绪期望只能包含一个 JSON 文档")
		}
		if err := expectation.Validate(); err != nil || expectation.TenantID != callCtx.GetTenantId() {
			return nil, nil, errors.New("节点就绪期望与认证租户不匹配")
		}
		observation, err := observer.Observe(ctx, expectation)
		if err != nil {
			return nil, nil, errors.New("可信节点就绪观察失败")
		}
		raw, err := json.Marshal(observation)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

func kernelNodeBootstrap(broker nodebootstrap.Broker) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != nodebootstrap.DeploymentManagerPluginID {
			return nil, nil, fmt.Errorf("kernel.node.bootstrap 只接受 deployment-manager 认证会话")
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		var request nodebootstrap.ExecutionRequest
		if err := decoder.Decode(&request); err != nil {
			return nil, nil, fmt.Errorf("节点引导计划无效: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, nil, errors.New("节点引导计划只能包含一个 JSON 文档")
		}
		if err := request.Validate(); err != nil || request.Plan.Node.Tenant != callCtx.GetTenantId() {
			return nil, nil, errors.New("节点引导计划与认证租户不匹配")
		}
		fence, err := deploymentManagerFence(ctx, callCtx, "node-bootstrap/"+request.OperationID)
		if err != nil {
			return nil, nil, err
		}
		scope := nodebootstrap.Scope{TenantID: callCtx.GetTenantId(), ProjectID: callCtx.GetProjectId(), PluginID: callCtx.GetCaller().GetId()}
		if err := scope.Validate(); err != nil {
			return nil, nil, err
		}
		result, err := broker.Bootstrap(ctx, scope, fence, request.Plan)
		if err != nil {
			return nil, nil, errors.New("可信节点引导执行失败")
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

type configGetRequest struct {
	Key string `json:"key"`
}

type managedCredentialRefRequest struct {
	FieldID string `json:"fieldId"`
}

func kernelManagedCredentialRef(provider kernelspi.ManagedCredentialRefProvider) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() == "" {
			return nil, nil, fmt.Errorf("kernel.config.credential-ref 只接受已认证插件会话")
		}
		var request managedCredentialRefRequest
		if err := json.Unmarshal(payload, &request); err != nil || strings.TrimSpace(request.FieldID) == "" {
			return nil, nil, fmt.Errorf("托管凭证字段不能为空")
		}
		ref, ok, err := provider.LookupManagedCredential(ctx, callCtx.GetCaller().GetId(), request.FieldID)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, kernelspi.ErrNotFound
		}
		raw, err := json.Marshal(ref)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

func kernelConfigGet(provider kernelspi.ConfigProvider) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() == "" {
			return nil, nil, fmt.Errorf("kernel.config.get 只接受已认证插件会话")
		}
		var request configGetRequest
		if err := json.Unmarshal(payload, &request); err != nil || request.Key == "" {
			return nil, nil, fmt.Errorf("配置请求 key 不能为空")
		}
		value, ok, err := provider.Lookup(ctx, callCtx.GetCaller().GetId(), request.Key)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, kernelspi.ErrNotFound
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, value, nil
	}
}

func kernelDiagnostics(host *protocolbus.Host) protocolbus.HostService {
	return func(_ context.Context, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
		out, err := json.Marshal(host.DiagnosticSnapshot())
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, out, nil
	}
}

func kernelInfo(version string) protocolbus.HostService {
	return func(_ context.Context, callCtx *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
		out, _ := json.Marshal(map[string]any{
			"kernel":     KernelName,
			"version":    version,
			"callerKind": callCtx.GetCaller().GetKind().String(),
			"tenant":     callCtx.GetTenantId(),
		})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, out, nil
	}
}
