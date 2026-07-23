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
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
	"cdsoft.com.cn/VastPlan/core/shared/go/runtimeidentity"
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
	return host, nil
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
		var plan nodebootstrap.Plan
		if err := decoder.Decode(&plan); err != nil {
			return nil, nil, fmt.Errorf("节点引导计划无效: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, nil, errors.New("节点引导计划只能包含一个 JSON 文档")
		}
		if err := plan.Validate(); err != nil || plan.Node.Tenant != callCtx.GetTenantId() {
			return nil, nil, errors.New("节点引导计划与认证租户不匹配")
		}
		scope := nodebootstrap.Scope{TenantID: callCtx.GetTenantId(), ProjectID: callCtx.GetProjectId(), PluginID: callCtx.GetCaller().GetId()}
		if err := scope.Validate(); err != nil {
			return nil, nil, err
		}
		result, err := broker.Bootstrap(ctx, scope, plan)
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
