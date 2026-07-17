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
// 继续提高；任何一方都不能把未知发布者降级到 trusted-process。
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

// ExecutionPolicy 将发布者来源转成宿主强制的隔离下限。零值保持既有第一方行为；
// 生产入口会显式配置 vastplan 为第一方，并要求所有未知发布者至少 process-sandbox。
type ExecutionPolicy struct {
	FirstPartyPublishers               map[string]struct{}
	RequireIsolationForOtherPublishers bool
}

func NewExecutionPolicy(firstParty []string, requireOtherIsolation bool) ExecutionPolicy {
	policy := ExecutionPolicy{FirstPartyPublishers: map[string]struct{}{}, RequireIsolationForOtherPublishers: requireOtherIsolation}
	for _, publisher := range firstParty {
		if publisher = strings.TrimSpace(publisher); publisher != "" {
			policy.FirstPartyPublishers[publisher] = struct{}{}
		}
	}
	return policy
}

func (p ExecutionPolicy) RequiredIsolation(plugin InstalledPlugin) (IsolationLevel, error) {
	required := IsolationLevel(plugin.Execution.MinimumIsolation)
	if _, ok := isolationRank[required]; !ok {
		return "", fmt.Errorf("插件 %s 的最低隔离等级无效: %s", plugin.ID, required)
	}
	if p.RequireIsolationForOtherPublishers {
		if _, firstParty := p.FirstPartyPublishers[plugin.Publisher]; !firstParty && isolationRank[required] < isolationRank[IsolationProcessSandbox] {
			required = IsolationProcessSandbox
		}
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
