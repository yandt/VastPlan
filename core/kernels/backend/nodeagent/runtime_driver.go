package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

// IsolationLevel 按强度单调递增。插件清单只能提高最低要求，节点发布者策略还能
// 继续提高；宿主策略不能降低签名清单声明的下限。
type IsolationLevel string

const (
	IsolationTrustedProcess IsolationLevel = "trusted-process"
	IsolationTrustedRuntime IsolationLevel = "trusted-runtime"
	IsolationProcessSandbox IsolationLevel = "process-sandbox"
	IsolationContainer      IsolationLevel = "container"
	IsolationWASM           IsolationLevel = "wasm"
)

var isolationRank = map[IsolationLevel]int{
	IsolationTrustedProcess: 0,
	IsolationTrustedRuntime: 0,
	IsolationProcessSandbox: 1,
	IsolationContainer:      2,
	IsolationWASM:           2,
}

// PluginExecutionDriver 启动一个已验签、已安装的插件，并返回统一实例句柄。
// 进程、Runtime Host、WASM 和内嵌实现都必须在这里完成最后一跳，主生命周期
// 不再根据语言生成条件分支。
type PluginExecutionDriver interface {
	Name() string
	Isolation() IsolationLevel
	Start(context.Context, *protocolbus.Host, InstalledPlugin, protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error)
}

// managedRuntimeExecutionDriver is implemented by providers capable of
// hosting multiple logical execution units in one physical process.
type managedRuntimeExecutionDriver interface {
	PluginExecutionDriver
	StartManaged(context.Context, *protocolbus.Host, InstalledPlugin, protocolbus.LaunchPolicy,
		*RuntimePoolManager, RuntimeHostingPolicy) (*protocolbus.PluginInstance, error)
}

type ExecutionDriverRegistry struct {
	mu      sync.RWMutex
	drivers map[string]PluginExecutionDriver
}

func NewExecutionDriverRegistry(drivers ...PluginExecutionDriver) (*ExecutionDriverRegistry, error) {
	registry := &ExecutionDriverRegistry{drivers: map[string]PluginExecutionDriver{}}
	for _, driver := range drivers {
		if err := registry.Register(driver); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func DefaultExecutionDrivers() *ExecutionDriverRegistry {
	nodeHost := strings.TrimSpace(os.Getenv("VASTPLAN_NODE_WORKER_HOST"))
	pythonHost := strings.TrimSpace(os.Getenv("VASTPLAN_PYTHON_SUBINTERPRETER_HOST"))
	nodeDriver := NodeWorkerExecutionDriver{Command: "vastplan-node-worker-host"}
	if nodeHost != "" {
		nodeDriver = NodeWorkerExecutionDriver{Command: "node", HostArgs: []string{nodeHost}}
	}
	pythonDriver := PythonSubinterpreterExecutionDriver{Command: "vastplan-python-subinterpreter-host"}
	if pythonHost != "" {
		pythonDriver = PythonSubinterpreterExecutionDriver{Command: "python3", HostArgs: []string{pythonHost}}
	}
	drivers := []PluginExecutionDriver{
		NativeExecutionDriver{},
		PythonProcessExecutionDriver{Interpreter: "python3"},
		nodeDriver,
		pythonDriver,
	}
	drivers = append(drivers, configuredIsolationDrivers()...)
	registry, _ := NewExecutionDriverRegistry(drivers...)
	return registry
}

func (r *ExecutionDriverRegistry) Register(driver PluginExecutionDriver) error {
	if r == nil || driver == nil || strings.TrimSpace(driver.Name()) == "" {
		return errors.New("运行驱动及名称不能为空")
	}
	if _, ok := isolationRank[driver.Isolation()]; !ok {
		return fmt.Errorf("运行驱动 %s 的隔离等级无效: %s", driver.Name(), driver.Isolation())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.drivers[driver.Name()]; exists {
		return fmt.Errorf("运行驱动重复: %s", driver.Name())
	}
	r.drivers[driver.Name()] = driver
	return nil
}

func (r *ExecutionDriverRegistry) Resolve(name string) (PluginExecutionDriver, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	driver, ok := r.drivers[name]
	return driver, ok
}

func (r *ExecutionDriverRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.drivers))
	for name := range r.drivers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type NativeExecutionDriver struct{}

func (NativeExecutionDriver) Name() string              { return "native" }
func (NativeExecutionDriver) Isolation() IsolationLevel { return IsolationTrustedProcess }
func (NativeExecutionDriver) Start(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error) {
	return host.LaunchSpecWithPolicy(ctx, processLaunchSpec(plugin, plugin.EntryPath, plugin.Execution.Args, "process"), policy)
}

type PythonProcessExecutionDriver struct{ Interpreter string }

func (PythonProcessExecutionDriver) Name() string              { return "python" }
func (PythonProcessExecutionDriver) Isolation() IsolationLevel { return IsolationTrustedProcess }
func (d PythonProcessExecutionDriver) Start(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error) {
	interpreter := strings.TrimSpace(d.Interpreter)
	if interpreter == "" {
		return nil, errors.New("python 执行驱动未配置解释器")
	}
	args := append([]string{plugin.EntryPath}, plugin.Execution.Args...)
	return host.LaunchSpecWithPolicy(ctx, processLaunchSpec(plugin, interpreter, args, "process"), policy)
}

// NodeWorkerExecutionDriver 由内核发布物中的受信任 Runtime Host 创建 Worker。
// Runtime Host 仍通过同一插件协议回连，不能绕过 LaunchPolicy。
type NodeWorkerExecutionDriver struct {
	Command  string
	HostArgs []string
}

func (NodeWorkerExecutionDriver) Name() string              { return "node-worker" }
func (NodeWorkerExecutionDriver) Isolation() IsolationLevel { return IsolationTrustedRuntime }
func (d NodeWorkerExecutionDriver) Start(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error) {
	if plugin.Execution.Node == nil || !plugin.Execution.Node.WorkerSafe || plugin.Execution.Node.ModuleFormat != "esm" {
		return nil, fmt.Errorf("插件 %s 未声明 node.workerSafe=true 且 moduleFormat=esm", plugin.ID)
	}
	command := strings.TrimSpace(d.Command)
	if command == "" {
		return nil, errors.New("node-worker 执行驱动未配置 Runtime Host")
	}
	args := append(append([]string(nil), d.HostArgs...), "--entry", plugin.EntryPath, "--")
	args = append(args, plugin.Execution.Args...)
	return host.LaunchSpecWithPolicy(ctx, processLaunchSpec(plugin, command, args, "node-worker"), policy)
}

func (d NodeWorkerExecutionDriver) StartManaged(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy, pools *RuntimePoolManager,
	hosting RuntimeHostingPolicy) (*protocolbus.PluginInstance, error) {
	if plugin.Execution.Node == nil || !plugin.Execution.Node.WorkerSafe || plugin.Execution.Node.ModuleFormat != "esm" {
		return nil, fmt.Errorf("插件 %s 未声明 node.workerSafe=true 且 moduleFormat=esm", plugin.ID)
	}
	return startManagedRuntime(ctx, host, plugin, policy, d, pools, hosting,
		runtimeHostProcessSpec{Command: d.Command, Args: append(append([]string(nil), d.HostArgs...), "--pool"), Kind: d.Name()})
}

// PythonSubinterpreterExecutionDriver 只接收对完整依赖图作出多解释器安全承诺
// 的插件。CPython 版本和扩展模块支持度由 Runtime Host 在握手前继续校验。
type PythonSubinterpreterExecutionDriver struct {
	Command  string
	HostArgs []string
}

func (PythonSubinterpreterExecutionDriver) Name() string { return "python-subinterpreter" }
func (PythonSubinterpreterExecutionDriver) Isolation() IsolationLevel {
	return IsolationTrustedRuntime
}
func (d PythonSubinterpreterExecutionDriver) Start(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error) {
	if plugin.Execution.Python == nil || !plugin.Execution.Python.SubinterpreterSafe {
		return nil, fmt.Errorf("插件 %s 未声明 python.subinterpreterSafe=true", plugin.ID)
	}
	command := strings.TrimSpace(d.Command)
	if command == "" {
		return nil, errors.New("python-subinterpreter 执行驱动未配置 Runtime Host")
	}
	args := append(append([]string(nil), d.HostArgs...), "--entry", plugin.EntryPath, "--")
	args = append(args, plugin.Execution.Args...)
	return host.LaunchSpecWithPolicy(ctx, processLaunchSpec(plugin, command, args, "python-subinterpreter"), policy)
}

func (d PythonSubinterpreterExecutionDriver) StartManaged(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy, pools *RuntimePoolManager,
	hosting RuntimeHostingPolicy) (*protocolbus.PluginInstance, error) {
	if plugin.Execution.Python == nil || !plugin.Execution.Python.SubinterpreterSafe {
		return nil, fmt.Errorf("插件 %s 未声明 python.subinterpreterSafe=true", plugin.ID)
	}
	return startManagedRuntime(ctx, host, plugin, policy, d, pools, hosting,
		runtimeHostProcessSpec{Command: d.Command, Args: append(append([]string(nil), d.HostArgs...), "--pool"), Kind: d.Name()})
}

func startManagedRuntime(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy, driver PluginExecutionDriver, pools *RuntimePoolManager,
	hosting RuntimeHostingPolicy, processSpec runtimeHostProcessSpec) (*protocolbus.PluginInstance, error) {
	if pools == nil {
		return nil, errors.New("托管运行驱动未配置 Runtime Pool Manager")
	}
	if strings.TrimSpace(processSpec.Command) == "" {
		return nil, fmt.Errorf("%s 执行驱动未配置 Runtime Host", driver.Name())
	}
	mode := hosting.modeFor(plugin)
	if err := validateRuntimeHostingMode(mode); err != nil {
		return nil, err
	}
	scope := strings.TrimSpace(policy.RuntimeScope)
	if scope == "" {
		scope = "default"
	}
	lease, err := pools.Acquire(runtimePoolKey(scope, plugin, driver, mode), processSpec)
	if err != nil {
		return nil, err
	}
	extraEnvironment := []string{
		"VASTPLAN_PLUGIN_ROOT=" + plugin.Root,
		"VASTPLAN_PLUGIN_DRIVER=" + plugin.Execution.Driver,
	}
	return host.LaunchManagedWithPolicy(ctx, protocolbus.ManagedLaunchSpec{
		PID: lease.PID(), RuntimeKind: driver.Name(),
		Start: func(environment []string) error {
			environment = append(append([]string(nil), environment...), extraEnvironment...)
			return lease.Start(ctx, plugin.EntryPath, plugin.Execution.Args, environment)
		},
		Stop: lease.Release,
		Done: lease.Done(),
	}, policy)
}

func processLaunchSpec(plugin InstalledPlugin, command string, args []string, kind string) protocolbus.LaunchSpec {
	return protocolbus.LaunchSpec{
		Command: command, Args: append([]string(nil), args...), Dir: plugin.Root,
		ExtraEnv:    []string{"VASTPLAN_PLUGIN_ROOT=" + plugin.Root, "VASTPLAN_PLUGIN_DRIVER=" + plugin.Execution.Driver},
		RuntimeKind: kind,
	}
}

// PublisherPluginPolicy 是内核使用者对某个发布者插件的运行决策。
// allow-trusted 只允许使用可信进程驱动，不会降低签名清单声明的 minimumIsolation。
type PublisherPluginPolicy string

const (
	PublisherPolicyRequireIsolation PublisherPluginPolicy = "require-isolation"
	PublisherPolicyAllowTrusted     PublisherPluginPolicy = "allow-trusted"
	PublisherPolicyDeny             PublisherPluginPolicy = "deny"
)

func validatePublisherPluginPolicy(policy PublisherPluginPolicy) error {
	switch policy {
	case PublisherPolicyRequireIsolation, PublisherPolicyAllowTrusted, PublisherPolicyDeny:
		return nil
	default:
		return fmt.Errorf("插件发布者策略无效: %q（可选: %s, %s, %s）", policy,
			PublisherPolicyRequireIsolation, PublisherPolicyAllowTrusted, PublisherPolicyDeny)
	}
}

// ExecutionPolicy 由内核使用者配置全局策略和发布者级覆盖。发布者级规则优先；
// 零值只为嵌入兼容而等价于 allow-trusted，生产入口会显式使用安全默认值。
type ExecutionPolicy struct {
	DefaultPolicy     PublisherPluginPolicy
	PublisherPolicies map[string]PublisherPluginPolicy
}

// ParseExecutionPolicy 解析生产配置。trustedPublishers 是旧
// -first-party-publishers 参数的兼容输入，只在没有显式发布者规则时补 allow-trusted。
func ParseExecutionPolicy(defaultPolicy, publisherPolicies string, trustedPublishers []string) (ExecutionPolicy, error) {
	policy := ExecutionPolicy{
		DefaultPolicy:     PublisherPluginPolicy(strings.TrimSpace(defaultPolicy)),
		PublisherPolicies: map[string]PublisherPluginPolicy{},
	}
	if err := validatePublisherPluginPolicy(policy.DefaultPolicy); err != nil {
		return ExecutionPolicy{}, err
	}
	if strings.TrimSpace(publisherPolicies) != "" {
		for _, rawRule := range strings.Split(publisherPolicies, ",") {
			publisher, rawPolicy, ok := strings.Cut(rawRule, "=")
			publisher = strings.TrimSpace(publisher)
			pluginPolicy := PublisherPluginPolicy(strings.TrimSpace(rawPolicy))
			if !ok || publisher == "" || pluginPolicy == "" {
				return ExecutionPolicy{}, fmt.Errorf("发布者策略格式无效: %q（应为 publisher=policy）", rawRule)
			}
			if _, exists := policy.PublisherPolicies[publisher]; exists {
				return ExecutionPolicy{}, fmt.Errorf("发布者策略重复: %s", publisher)
			}
			if err := validatePublisherPluginPolicy(pluginPolicy); err != nil {
				return ExecutionPolicy{}, fmt.Errorf("发布者 %s: %w", publisher, err)
			}
			policy.PublisherPolicies[publisher] = pluginPolicy
		}
	}
	for _, publisher := range trustedPublishers {
		publisher = strings.TrimSpace(publisher)
		if publisher == "" {
			continue
		}
		if _, explicitlyConfigured := policy.PublisherPolicies[publisher]; !explicitlyConfigured {
			policy.PublisherPolicies[publisher] = PublisherPolicyAllowTrusted
		}
	}
	return policy, nil
}

func (p ExecutionPolicy) policyFor(publisher string) PublisherPluginPolicy {
	if policy, ok := p.PublisherPolicies[publisher]; ok {
		return policy
	}
	if p.DefaultPolicy == "" {
		return PublisherPolicyAllowTrusted
	}
	return p.DefaultPolicy
}

func (p ExecutionPolicy) RequiredIsolation(plugin InstalledPlugin) (IsolationLevel, error) {
	required := IsolationLevel(plugin.Execution.MinimumIsolation)
	if _, ok := isolationRank[required]; !ok {
		return "", fmt.Errorf("插件 %s 的最低隔离等级无效: %s", plugin.ID, required)
	}
	switch policy := p.policyFor(plugin.Publisher); policy {
	case PublisherPolicyDeny:
		return "", fmt.Errorf("插件 %s 的发布者 %s 被内核运行策略拒绝", plugin.ID, plugin.Publisher)
	case PublisherPolicyRequireIsolation:
		if isolationRank[required] < isolationRank[IsolationProcessSandbox] {
			required = IsolationProcessSandbox
		}
	case PublisherPolicyAllowTrusted:
		// 签名清单 minimumIsolation 仍是不可降低的下限。
	default:
		return "", fmt.Errorf("发布者 %s: %w", plugin.Publisher, validatePublisherPluginPolicy(policy))
	}
	return required, nil
}

func normalizeExecutionContract(plugin InstalledPlugin) InstalledPlugin {
	// 兼容升级前实际态和直接构造 InstalledPlugin 的嵌入方；正式安装路径已经
	// 冻结同样的默认值，这里是运行边界上的最后一道归一化。
	if strings.TrimSpace(plugin.Execution.Driver) == "" {
		plugin.Execution.Driver = "native"
	}
	if strings.TrimSpace(plugin.Execution.MinimumIsolation) == "" {
		plugin.Execution.MinimumIsolation = string(IsolationTrustedProcess)
	}
	return plugin
}

func validateExecutionPlatform(plugin InstalledPlugin) error {
	if len(plugin.Execution.Platforms) != 0 {
		current := runtime.GOOS + "/" + runtime.GOARCH
		matched := false
		for _, platform := range plugin.Execution.Platforms {
			matched = matched || platform == current
		}
		if !matched {
			return fmt.Errorf("插件 %s 不支持当前平台 %s", plugin.ID, current)
		}
	}
	return nil
}

func (r *ProtocolRuntime) resolveExecutionDriver(plugin InstalledPlugin) (PluginExecutionDriver, InstalledPlugin, error) {
	plugin = normalizeExecutionContract(plugin)
	if err := validateExecutionPlatform(plugin); err != nil {
		return nil, InstalledPlugin{}, err
	}
	drivers := r.Drivers
	if drivers == nil {
		drivers = DefaultExecutionDrivers()
	}
	driver, ok := drivers.Resolve(plugin.Execution.Driver)
	if !ok {
		return nil, InstalledPlugin{}, fmt.Errorf("插件 %s 请求未注册执行驱动 %q（可用: %s）", plugin.ID, plugin.Execution.Driver, strings.Join(drivers.Names(), ","))
	}
	required, err := r.ExecutionPolicy.RequiredIsolation(plugin)
	if err != nil {
		return nil, InstalledPlugin{}, err
	}
	if isolationRank[driver.Isolation()] < isolationRank[required] {
		return nil, InstalledPlugin{}, fmt.Errorf("插件 %s 发布者 %s 要求隔离 %s，驱动 %s 仅提供 %s", plugin.ID, plugin.Publisher, required, driver.Name(), driver.Isolation())
	}
	return driver, plugin, nil
}

func (r *ProtocolRuntime) startConfiguredPlugin(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error) {
	driver, normalized, err := r.resolveExecutionDriver(plugin)
	if err != nil {
		return nil, err
	}
	if managed, ok := driver.(managedRuntimeExecutionDriver); ok && r.RuntimePools != nil {
		return managed.StartManaged(ctx, host, normalized, policy, r.RuntimePools, r.HostingPolicy)
	}
	return driver.Start(ctx, host, normalized, policy)
}
