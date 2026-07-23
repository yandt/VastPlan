package nodeagent

import (
	"context"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type sandboxTestDriver struct{}

func (sandboxTestDriver) Name() string              { return "sandbox.test" }
func (sandboxTestDriver) Isolation() IsolationLevel { return IsolationProcessSandbox }
func (sandboxTestDriver) Start(context.Context, *protocolbus.Host, InstalledPlugin,
	protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error) {
	return nil, nil
}

func TestRuntimeDriversResolveLanguageAndEnforcePublisherIsolation(t *testing.T) {
	plugin := InstalledPlugin{
		ID: "com.example.python", Publisher: "vastplan", Root: "/plugins/python", EntryPath: "/plugins/python/main.py",
		Execution: pluginv1.BackendExecution{Driver: "python", Args: []string{"--worker"}, MinimumIsolation: "trusted-process"},
	}
	runtime := NewProtocolRuntime("1.0.0", nil)
	policy, err := ParseExecutionPolicy("require-isolation", "", []string{"vastplan"})
	if err != nil {
		t.Fatal(err)
	}
	runtime.ExecutionPolicy = policy
	driver, normalized, err := runtime.resolveExecutionDriver(plugin)
	if err != nil {
		t.Fatal(err)
	}
	if driver.Name() != "python" || normalized.Execution.Driver != "python" {
		t.Fatalf("python 执行驱动解析错误: driver=%s plugin=%+v", driver.Name(), normalized)
	}

	plugin.Publisher = "third-party"
	if _, _, err := runtime.resolveExecutionDriver(plugin); err == nil || !strings.Contains(err.Error(), "要求隔离") {
		t.Fatalf("未知发布者不能降级为 trusted-process: %v", err)
	}

	registry, err := NewExecutionDriverRegistry(sandboxTestDriver{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.Drivers = registry
	plugin.Execution.Driver = "sandbox.test"
	if _, _, err := runtime.resolveExecutionDriver(plugin); err != nil {
		t.Fatalf("隔离驱动应允许第三方发布者: %v", err)
	}
}

func TestPythonLaunchSpecUsesOnlyMaterializedDependencyOverlay(t *testing.T) {
	plugin := InstalledPlugin{
		Root: "/plugins/python", EntryPath: "/plugins/python/backend/main.py",
		PythonPath: "/plugins/python/.vastplan/python/site-packages",
		Execution:  pluginv1.BackendExecution{Driver: "python"},
	}
	spec := processLaunchSpec(plugin, "python3", []string{plugin.EntryPath}, "process")
	joined := strings.Join(spec.ExtraEnv, "\n")
	if !strings.Contains(joined, "VASTPLAN_PYTHON_DEPENDENCIES="+plugin.PythonPath) || !strings.Contains(joined, "PYTHONPATH="+plugin.PythonPath) || !strings.Contains(joined, "PYTHONNOUSERSITE=1") {
		t.Fatalf("Python 启动未使用隔离依赖 overlay: %#v", spec.ExtraEnv)
	}
}

func TestManagedRuntimeDriversRequireExplicitCompatibilityDeclaration(t *testing.T) {
	nodeDriver := NodeWorkerExecutionDriver{Command: "node-host"}
	plugin := InstalledPlugin{ID: "com.example.node", EntryPath: "/plugin/main.mjs",
		Execution: pluginv1.BackendExecution{Driver: "node-worker"}}
	if _, err := nodeDriver.Start(context.Background(), nil, plugin, protocolbus.LaunchPolicy{}); err == nil ||
		!strings.Contains(err.Error(), "workerSafe") {
		t.Fatalf("Node Worker 缺少兼容声明必须 fail-closed: %v", err)
	}

	pythonDriver := PythonSubinterpreterExecutionDriver{Command: "python-host"}
	plugin.ID = "com.example.python"
	plugin.Execution = pluginv1.BackendExecution{Driver: "python-subinterpreter"}
	if _, err := pythonDriver.Start(context.Background(), nil, plugin, protocolbus.LaunchPolicy{}); err == nil ||
		!strings.Contains(err.Error(), "subinterpreterSafe") {
		t.Fatalf("Python 子解释器缺少兼容声明必须 fail-closed: %v", err)
	}
}

func TestDefaultManagedRuntimeDriversUseTrustedHostOverrides(t *testing.T) {
	t.Setenv("VASTPLAN_NODE_WORKER_HOST", "/kernel/runtimehosts/node/host.mjs")
	t.Setenv("VASTPLAN_PYTHON_SUBINTERPRETER_HOST", "/kernel/runtimehosts/python/host.py")
	registry := DefaultExecutionDrivers()
	node, ok := registry.Resolve("node-worker")
	if !ok {
		t.Fatal("缺少 node-worker 驱动")
	}
	nodeDriver := node.(NodeWorkerExecutionDriver)
	if nodeDriver.Command != "node" || len(nodeDriver.HostArgs) != 1 || nodeDriver.HostArgs[0] != "/kernel/runtimehosts/node/host.mjs" {
		t.Fatalf("Node Runtime Host 覆盖未生效: %+v", nodeDriver)
	}
	python, ok := registry.Resolve("python-subinterpreter")
	if !ok {
		t.Fatal("缺少 python-subinterpreter 驱动")
	}
	pythonDriver := python.(PythonSubinterpreterExecutionDriver)
	if pythonDriver.Command != "python3" || len(pythonDriver.HostArgs) != 1 || pythonDriver.HostArgs[0] != "/kernel/runtimehosts/python/host.py" {
		t.Fatalf("Python Runtime Host 覆盖未生效: %+v", pythonDriver)
	}
}

func TestExecutionPolicyPublisherOverridePrecedenceAndManifestFloor(t *testing.T) {
	policy, err := ParseExecutionPolicy(
		"require-isolation",
		"vastplan=deny,partner=allow-trusted,sensitive=require-isolation",
		[]string{"vastplan"},
	)
	if err != nil {
		t.Fatal(err)
	}
	plugin := InstalledPlugin{ID: "p", Execution: pluginv1.BackendExecution{MinimumIsolation: "trusted-process"}}

	plugin.Publisher = "vastplan"
	if _, err := policy.RequiredIsolation(plugin); err == nil || !strings.Contains(err.Error(), "被内核运行策略拒绝") {
		t.Fatalf("显式 deny 必须覆盖旧第一方兼容名单: %v", err)
	}

	plugin.Publisher = "partner"
	if got, err := policy.RequiredIsolation(plugin); err != nil || got != IsolationTrustedProcess {
		t.Fatalf("发布者 allow-trusted 必须覆盖全局隔离策略: isolation=%s err=%v", got, err)
	}

	plugin.Publisher = "unknown"
	if got, err := policy.RequiredIsolation(plugin); err != nil || got != IsolationProcessSandbox {
		t.Fatalf("未匹配发布者必须使用全局策略: isolation=%s err=%v", got, err)
	}

	plugin.Publisher = "partner"
	plugin.Execution.MinimumIsolation = "container"
	if got, err := policy.RequiredIsolation(plugin); err != nil || got != IsolationContainer {
		t.Fatalf("allow-trusted 不得降低签名清单下限: isolation=%s err=%v", got, err)
	}

	plugin.Publisher = "sensitive"
	plugin.Execution.MinimumIsolation = "trusted-process"
	if got, err := policy.RequiredIsolation(plugin); err != nil || got != IsolationProcessSandbox {
		t.Fatalf("发布者 require-isolation 必须提高隔离下限: isolation=%s err=%v", got, err)
	}
}

func TestParseExecutionPolicyRejectsInvalidOrAmbiguousRules(t *testing.T) {
	tests := []struct {
		global, publishers string
	}{
		{global: "invalid"},
		{global: "deny", publishers: "missing-separator"},
		{global: "deny", publishers: "acme=invalid"},
		{global: "deny", publishers: "acme=deny,acme=allow-trusted"},
	}
	for _, test := range tests {
		if _, err := ParseExecutionPolicy(test.global, test.publishers, nil); err == nil {
			t.Fatalf("无效/歧义策略必须拒绝: %+v", test)
		}
	}
}

func TestRuntimeHostingPolicyDefaultsToSharedAndUsesNarrowOverride(t *testing.T) {
	policy, err := ParseRuntimeHostingPolicy(
		"shared", "vastplan=dedicated,partner=shared", "cn.vastplan.fast=shared",
	)
	if err != nil {
		t.Fatal(err)
	}
	plugin := InstalledPlugin{ID: "cn.vastplan.fast", Publisher: "vastplan"}
	if got := policy.modeFor(plugin); got != RuntimeHostingShared {
		t.Fatalf("插件规则必须优先于发布者规则: %s", got)
	}
	plugin.ID = "cn.vastplan.other"
	if got := policy.modeFor(plugin); got != RuntimeHostingDedicated {
		t.Fatalf("发布者规则未生效: %s", got)
	}
	plugin.Publisher = "unknown"
	if got := policy.modeFor(plugin); got != RuntimeHostingShared {
		t.Fatalf("默认应共享 Runtime Host: %s", got)
	}
}

func TestRuntimeHostingPolicyRejectsInvalidAndDuplicateRules(t *testing.T) {
	for _, test := range []struct{ defaults, publishers, plugins string }{
		{defaults: "invalid"},
		{defaults: "shared", publishers: "vastplan=invalid"},
		{defaults: "shared", publishers: "vastplan=shared,vastplan=dedicated"},
		{defaults: "shared", plugins: "missing-separator"},
	} {
		if _, err := ParseRuntimeHostingPolicy(test.defaults, test.publishers, test.plugins); err == nil {
			t.Fatalf("无效 Runtime Host 策略必须拒绝: %+v", test)
		}
	}
}

func TestRuntimePoolCompatibilitySeparatesPublisherAndRequirements(t *testing.T) {
	driver := NodeWorkerExecutionDriver{Command: "node"}
	plugin := InstalledPlugin{ID: "cn.vastplan.a", Publisher: "vastplan", Execution: pluginv1.BackendExecution{
		Driver: "node-worker", Requirements: map[string]string{"node": ">=20"},
		Node: &pluginv1.NodeExecution{WorkerSafe: true, ModuleFormat: "esm"},
	}}
	base := runtimePoolKey("service-a", plugin, driver, RuntimeHostingShared)
	otherPublisher := plugin
	otherPublisher.Publisher = "partner"
	if base.String() == runtimePoolKey("service-a", otherPublisher, driver, RuntimeHostingShared).String() {
		t.Fatal("不同发布者信任域不得共享 Runtime Host")
	}
	otherRuntime := plugin
	otherRuntime.Execution.Requirements = map[string]string{"node": ">=22"}
	if base.String() == runtimePoolKey("service-a", otherRuntime, driver, RuntimeHostingShared).String() {
		t.Fatal("不同运行时约束不得共享 Runtime Host")
	}
	if runtimePoolKey("service-b", plugin, driver, RuntimeHostingShared).String() == base.String() {
		t.Fatal("不同内核服务不得共享 Runtime Host")
	}
}

func TestRuntimeDriversRejectUnknownDriverAndUnsupportedPlatform(t *testing.T) {
	runtime := NewProtocolRuntime("1.0.0", nil)
	plugin := InstalledPlugin{ID: "p", Publisher: "vastplan", EntryPath: "/p", Execution: pluginv1.BackendExecution{
		Driver: "missing", MinimumIsolation: "trusted-process",
	}}
	if _, _, err := runtime.resolveExecutionDriver(plugin); err == nil || !strings.Contains(err.Error(), "未注册执行驱动") {
		t.Fatalf("未知驱动必须 fail-closed: %v", err)
	}
	plugin.Execution.Driver = "native"
	plugin.Execution.Platforms = []string{"unsupported/none"}
	if _, _, err := runtime.resolveExecutionDriver(plugin); err == nil || !strings.Contains(err.Error(), "不支持当前平台") {
		t.Fatalf("平台不匹配必须 fail-closed: %v", err)
	}
}
