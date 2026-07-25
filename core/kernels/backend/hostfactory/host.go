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
	reg := backendRegistry()
	host := protocolbus.NewHost(KernelName, version, reg, logf)
	services := []hostServiceRegistration{
		{name: "kernel.info", handler: kernelInfo(version)},
		{name: "kernel.diagnostics", handler: kernelDiagnostics(host)},
	}
	services = append(services, dependencyHostServices(dependencies)...)
	services = append(services, platformProfileActivationServices(dependencies)...)
	services = append(services, configurationHostServices(dependencies)...)
	services = append(services, sharedStateServices(dependencies, host)...)
	if err := registerHostServices(host, services); err != nil {
		return nil, err
	}
	return host, nil
}

func backendRegistry() *registry.Registry {
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
	return reg
}

type hostServiceRegistration struct {
	name    string
	handler protocolbus.HostService
}

func dependencyHostServices(dependencies kernelspi.Dependencies) []hostServiceRegistration {
	services := make([]hostServiceRegistration, 0, 9)
	if dependencies.Config != nil {
		services = append(services, hostServiceRegistration{name: "kernel.config.get", handler: kernelConfigGet(dependencies.Config)})
	}
	if dependencies.ManagedCredentialRefs != nil {
		services = append(services, hostServiceRegistration{name: pluginconfig.KernelCredentialRefService, handler: kernelManagedCredentialRef(dependencies.ManagedCredentialRefs)})
	}
	if dependencies.RuntimeMaterialLeases != nil {
		services = append(services, hostServiceRegistration{name: credentiallease.RuntimeKernelService, handler: kernelRuntimeMaterialLease(dependencies.RuntimeMaterialLeases)})
	}
	if dependencies.NodeBootstrap != nil {
		services = append(services, hostServiceRegistration{name: nodebootstrap.KernelService, handler: kernelNodeBootstrap(dependencies.NodeBootstrap)})
	}
	if dependencies.NodeReadiness != nil {
		services = append(services, hostServiceRegistration{name: nodebootstrap.KernelReadinessService, handler: kernelNodeReadiness(dependencies.NodeReadiness)})
	}
	if dependencies.DeploymentPublication != nil {
		services = append(services,
			hostServiceRegistration{name: deploymentpublication.KernelTargetsService, handler: kernelDeploymentTargets(dependencies.DeploymentPublication)},
			hostServiceRegistration{name: deploymentpublication.KernelPreviewService, handler: kernelDeploymentPreview(dependencies.DeploymentPublication)},
			hostServiceRegistration{name: deploymentpublication.KernelPublishService, handler: kernelDeploymentPublish(dependencies.DeploymentPublication)},
		)
	}
	if dependencies.DeploymentReadiness != nil {
		services = append(services, hostServiceRegistration{name: deploymentpublication.KernelReadinessService, handler: kernelDeploymentReadiness(dependencies.DeploymentReadiness)})
	}
	return services
}

func platformProfileActivationServices(dependencies kernelspi.Dependencies) []hostServiceRegistration {
	if dependencies.PlatformProfileActivation == nil {
		return nil
	}
	handlers := kernelPlatformProfileActivation(dependencies.PlatformProfileActivation)
	names := make([]string, 0, len(handlers))
	for name := range handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	services := make([]hostServiceRegistration, 0, len(names))
	for _, name := range names {
		services = append(services, hostServiceRegistration{name: name, handler: handlers[name]})
	}
	return services
}

func configurationHostServices(dependencies kernelspi.Dependencies) []hostServiceRegistration {
	services := make([]hostServiceRegistration, 0, 3)
	if dependencies.ConfigurationCatalogs != nil {
		services = append(services, hostServiceRegistration{name: pluginconfiguration.KernelCatalogsService, handler: kernelConfigurationCatalogs(dependencies.ConfigurationCatalogs)})
	}
	if dependencies.ConfigurationAuthorityIssuer != nil {
		services = append(services, hostServiceRegistration{name: configurationauthority.KernelIssueService, handler: kernelConfigurationAuthorityIssue(dependencies.ConfigurationAuthorityIssuer)})
	}
	if dependencies.ConfigurationAuthorityConsumer != nil {
		services = append(services, hostServiceRegistration{name: configurationauthority.KernelConsumeService, handler: kernelConfigurationAuthorityConsume(dependencies.ConfigurationAuthorityConsumer)})
	}
	return services
}

func sharedStateServices(dependencies kernelspi.Dependencies, host *protocolbus.Host) []hostServiceRegistration {
	if dependencies.SharedState == nil {
		return nil
	}
	operations := []string{sharedstatev1.OperationGet, sharedstatev1.OperationCreate, sharedstatev1.OperationUpdate, sharedstatev1.OperationDelete, sharedstatev1.OperationList}
	services := make([]hostServiceRegistration, 0, len(operations)+3)
	for _, operation := range operations {
		services = append(services, hostServiceRegistration{name: sharedstatev1.KernelService(operation), handler: kernelSharedStateWithMetrics(dependencies.SharedState, operation, host.Observer.Metrics)})
	}
	for _, operation := range []string{sharedstatev1.OperationCreate, sharedstatev1.OperationUpdate, sharedstatev1.OperationDelete} {
		services = append(services, hostServiceRegistration{name: sharedstatev1.FencedKernelService(operation), handler: kernelFencedSharedStateWithMetrics(dependencies.SharedState, operation, host.Observer.Metrics)})
	}
	return services
}

func registerHostServices(host *protocolbus.Host, services []hostServiceRegistration) error {
	for _, service := range services {
		if err := host.RegisterHostService(extpoint.KernelService, service.name, service.handler); err != nil {
			return err
		}
	}
	return nil
}
