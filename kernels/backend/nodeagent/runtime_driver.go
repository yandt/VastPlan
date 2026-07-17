package nodeagent

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"

	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
)

// IsolationLevel 按强度单调递增。插件清单只能提高最低要求，节点发布者策略还能
// 继续提高；宿主策略不能降低签名清单声明的下限。
type IsolationLevel string

const (
	IsolationTrustedProcess IsolationLevel = "trusted-process"
	IsolationProcessSandbox IsolationLevel = "process-sandbox"
	IsolationContainer      IsolationLevel = "container"
	IsolationWASM           IsolationLevel = "wasm"
)

var isolationRank = map[IsolationLevel]int{
	IsolationTrustedProcess: 0,
	IsolationProcessSandbox: 1,
	IsolationContainer:      2,
	IsolationWASM:           2,
}

// PluginRuntimeDriver 把已验签、已安装的语言制品转换为无 shell LaunchSpec。
// OCI/WASM/系统沙箱实现未来注册到同一接口，协议宿主不感知具体运行时。
type PluginRuntimeDriver interface {
	Name() string
	Isolation() IsolationLevel
	LaunchSpec(InstalledPlugin) (protocolbus.LaunchSpec, error)
}

type RuntimeDriverRegistry struct {
	mu      sync.RWMutex
	drivers map[string]PluginRuntimeDriver
}

func NewRuntimeDriverRegistry(drivers ...PluginRuntimeDriver) (*RuntimeDriverRegistry, error) {
	registry := &RuntimeDriverRegistry{drivers: map[string]PluginRuntimeDriver{}}
	for _, driver := range drivers {
		if err := registry.Register(driver); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func DefaultRuntimeDrivers() *RuntimeDriverRegistry {
	registry, _ := NewRuntimeDriverRegistry(NativeRuntimeDriver{}, PythonRuntimeDriver{Interpreter: "python3"})
	return registry
}

func (r *RuntimeDriverRegistry) Register(driver PluginRuntimeDriver) error {
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

func (r *RuntimeDriverRegistry) Resolve(name string) (PluginRuntimeDriver, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	driver, ok := r.drivers[name]
	return driver, ok
}

func (r *RuntimeDriverRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.drivers))
	for name := range r.drivers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type NativeRuntimeDriver struct{}

func (NativeRuntimeDriver) Name() string              { return "native" }
func (NativeRuntimeDriver) Isolation() IsolationLevel { return IsolationTrustedProcess }
func (NativeRuntimeDriver) LaunchSpec(plugin InstalledPlugin) (protocolbus.LaunchSpec, error) {
	return launchSpec(plugin, plugin.EntryPath, plugin.Execution.Args), nil
}

type PythonRuntimeDriver struct{ Interpreter string }

func (PythonRuntimeDriver) Name() string              { return "python" }
func (PythonRuntimeDriver) Isolation() IsolationLevel { return IsolationTrustedProcess }
func (d PythonRuntimeDriver) LaunchSpec(plugin InstalledPlugin) (protocolbus.LaunchSpec, error) {
	interpreter := strings.TrimSpace(d.Interpreter)
	if interpreter == "" {
		return protocolbus.LaunchSpec{}, errors.New("python 运行驱动未配置解释器")
	}
	args := append([]string{plugin.EntryPath}, plugin.Execution.Args...)
	return launchSpec(plugin, interpreter, args), nil
}

func launchSpec(plugin InstalledPlugin, command string, args []string) protocolbus.LaunchSpec {
	return protocolbus.LaunchSpec{
		Command: command, Args: append([]string(nil), args...), Dir: plugin.Root,
		ExtraEnv: []string{"VASTPLAN_PLUGIN_ROOT=" + plugin.Root, "VASTPLAN_PLUGIN_DRIVER=" + plugin.Execution.Driver},
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

func (r *ProtocolRuntime) launchSpec(plugin InstalledPlugin) (protocolbus.LaunchSpec, error) {
	// 兼容升级前实际态和直接构造 InstalledPlugin 的嵌入方；正式安装路径已经
	// 冻结同样的默认值，这里是运行边界上的最后一道归一化。
	if strings.TrimSpace(plugin.Execution.Driver) == "" {
		plugin.Execution.Driver = "native"
	}
	if strings.TrimSpace(plugin.Execution.MinimumIsolation) == "" {
		plugin.Execution.MinimumIsolation = string(IsolationTrustedProcess)
	}
	if len(plugin.Execution.Platforms) != 0 {
		current := runtime.GOOS + "/" + runtime.GOARCH
		matched := false
		for _, platform := range plugin.Execution.Platforms {
			matched = matched || platform == current
		}
		if !matched {
			return protocolbus.LaunchSpec{}, fmt.Errorf("插件 %s 不支持当前平台 %s", plugin.ID, current)
		}
	}
	drivers := r.Drivers
	if drivers == nil {
		drivers = DefaultRuntimeDrivers()
	}
	driver, ok := drivers.Resolve(plugin.Execution.Driver)
	if !ok {
		return protocolbus.LaunchSpec{}, fmt.Errorf("插件 %s 请求未注册运行驱动 %q（可用: %s）", plugin.ID, plugin.Execution.Driver, strings.Join(drivers.Names(), ","))
	}
	required, err := r.ExecutionPolicy.RequiredIsolation(plugin)
	if err != nil {
		return protocolbus.LaunchSpec{}, err
	}
	if isolationRank[driver.Isolation()] < isolationRank[required] {
		return protocolbus.LaunchSpec{}, fmt.Errorf("插件 %s 发布者 %s 要求隔离 %s，驱动 %s 仅提供 %s", plugin.ID, plugin.Publisher, required, driver.Name(), driver.Isolation())
	}
	return driver.LaunchSpec(plugin)
}
