// Package hostfactory 集中声明 backend 内核的扩展点和内置能力。
//
// 手工演示入口与 Node Agent 自动装配必须使用同一宿主工厂；否则两条启动路径会
// 悄悄形成不同的内核能力集合。
package hostfactory

import (
	"sort"

	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	"cdsoft.com.cn/VastPlan/core/shared/go/credentiallease"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
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
			if err := host.RegisterHostService(extpoint.KernelService, sharedstatev1.KernelService(operation), kernelSharedStateWithMetrics(dependencies.SharedState, operation, host.Observer.Metrics)); err != nil {
				return nil, err
			}
		}
		for _, operation := range []string{
			sharedstatev1.OperationCreate, sharedstatev1.OperationUpdate, sharedstatev1.OperationDelete,
		} {
			if err := host.RegisterHostService(extpoint.KernelService, sharedstatev1.FencedKernelService(operation), kernelFencedSharedStateWithMetrics(dependencies.SharedState, operation, host.Observer.Metrics)); err != nil {
				return nil, err
			}
		}
	}
	return host, nil
}
