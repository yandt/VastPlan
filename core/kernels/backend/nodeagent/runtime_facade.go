package nodeagent

import (
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent/runtimehost"
	"cdsoft.com.cn/VastPlan/core/shared/go/processguard"
)

type ProtocolRuntime = runtimehost.ProtocolRuntime
type ContextPolicy = runtimehost.ContextPolicy
type PlacementMode = runtimehost.PlacementMode
type PlacementPolicy = runtimehost.PlacementPolicy
type DynamicGoExecutionDriver = runtimehost.DynamicGoExecutionDriver
type IsolationLevel = runtimehost.IsolationLevel
type PluginExecutionDriver = runtimehost.PluginExecutionDriver
type ExecutionDriverRegistry = runtimehost.ExecutionDriverRegistry
type NativeExecutionDriver = runtimehost.NativeExecutionDriver
type PythonProcessExecutionDriver = runtimehost.PythonProcessExecutionDriver
type NodeWorkerExecutionDriver = runtimehost.NodeWorkerExecutionDriver
type PythonSubinterpreterExecutionDriver = runtimehost.PythonSubinterpreterExecutionDriver
type PublisherPluginPolicy = runtimehost.PublisherPluginPolicy
type ExecutionPolicy = runtimehost.ExecutionPolicy
type IsolationExecutionDriver = runtimehost.IsolationExecutionDriver
type KernelServiceGrantPolicy = runtimehost.KernelServiceGrantPolicy
type RuntimeHostingMode = runtimehost.RuntimeHostingMode
type RuntimeHostingPolicy = runtimehost.RuntimeHostingPolicy
type RuntimeHostKey = runtimehost.RuntimeHostKey
type RuntimePoolSnapshot = runtimehost.RuntimePoolSnapshot
type RuntimePoolManager = runtimehost.RuntimePoolManager
type RuntimeHostLease = runtimehost.RuntimeHostLease
type SchemaActivationError = runtimehost.SchemaActivationError

const (
	PlacementProcessOnly      = runtimehost.PlacementProcessOnly
	PlacementPreferDynamicGo  = runtimehost.PlacementPreferDynamicGo
	PlacementRequireDynamicGo = runtimehost.PlacementRequireDynamicGo

	IsolationTrustedProcess = runtimehost.IsolationTrustedProcess
	IsolationTrustedRuntime = runtimehost.IsolationTrustedRuntime
	IsolationProcessSandbox = runtimehost.IsolationProcessSandbox
	IsolationContainer      = runtimehost.IsolationContainer
	IsolationWASM           = runtimehost.IsolationWASM

	PublisherPolicyAllowTrusted     = runtimehost.PublisherPolicyAllowTrusted
	PublisherPolicyRequireIsolation = runtimehost.PublisherPolicyRequireIsolation
	PublisherPolicyDeny             = runtimehost.PublisherPolicyDeny

	RuntimeHostingShared    = runtimehost.RuntimeHostingShared
	RuntimeHostingDedicated = runtimehost.RuntimeHostingDedicated
)

func NewProtocolRuntime(kernelVersion string, logf func(string, ...any)) *ProtocolRuntime {
	return runtimehost.NewProtocolRuntime(kernelVersion, logf)
}

func DefaultContextPolicy() ContextPolicy {
	return runtimehost.DefaultContextPolicy()
}

func NewContextPolicy(defaultFields []string, publishers map[string][]string) (ContextPolicy, error) {
	return runtimehost.NewContextPolicy(defaultFields, publishers)
}

func ParseContextPolicy(defaultFields, publisherRules string) (ContextPolicy, error) {
	return runtimehost.ParseContextPolicy(defaultFields, publisherRules)
}

func ParsePlacementPolicy(defaultMode, publisherRules, pluginRules string) (PlacementPolicy, error) {
	return runtimehost.ParsePlacementPolicy(defaultMode, publisherRules, pluginRules)
}

func NewExecutionDriverRegistry(drivers ...PluginExecutionDriver) (*ExecutionDriverRegistry, error) {
	return runtimehost.NewExecutionDriverRegistry(drivers...)
}

func DefaultExecutionDrivers() *ExecutionDriverRegistry {
	return runtimehost.DefaultExecutionDrivers()
}

func ParseExecutionPolicy(defaultPolicy, publisherPolicies string, trustedPublishers []string) (ExecutionPolicy, error) {
	return runtimehost.ParseExecutionPolicy(defaultPolicy, publisherPolicies, trustedPublishers)
}

func ParseKernelServiceGrantPolicy(defaultServices, publisherRules string) (KernelServiceGrantPolicy, error) {
	return runtimehost.ParseKernelServiceGrantPolicy(defaultServices, publisherRules)
}

func ParseRuntimeHostingPolicy(defaultMode, publisherRules, pluginRules string) (RuntimeHostingPolicy, error) {
	return runtimehost.ParseRuntimeHostingPolicy(defaultMode, publisherRules, pluginRules)
}

func NewRuntimePoolManager(logf func(string, ...any)) *RuntimePoolManager {
	return runtimehost.NewRuntimePoolManager(logf)
}

func NewRuntimePoolManagerWithGuardian(logf func(string, ...any), guardian processguard.Guardian) *RuntimePoolManager {
	return runtimehost.NewRuntimePoolManagerWithGuardian(logf, guardian)
}
