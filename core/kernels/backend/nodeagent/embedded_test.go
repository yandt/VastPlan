package nodeagent

import (
	"context"
	"errors"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
)

func TestPlacementPolicyPrecedenceAndRejectsStaticModes(t *testing.T) {
	policy, err := ParsePlacementPolicy("process-only", "vastplan=prefer-dynamic-go",
		"cn.vastplan.foundation.security.bootstrap-policy=require-dynamic-go")
	if err != nil {
		t.Fatal(err)
	}
	plugin := InstalledPlugin{ID: "cn.vastplan.foundation.security.bootstrap-policy", Publisher: "vastplan"}
	if got := policy.modeFor(plugin); got != PlacementRequireDynamicGo {
		t.Fatalf("插件级规则应优先，实际 %s", got)
	}
	plugin.ID = "cn.vastplan.platform.settings"
	if got := policy.modeFor(plugin); got != PlacementPreferDynamicGo {
		t.Fatalf("发布者级规则应次优先，实际 %s", got)
	}
	for _, mode := range []string{"prefer-embedded", "require-embedded"} {
		if _, err := ParsePlacementPolicy(mode, "", ""); err == nil {
			t.Fatalf("静态内嵌模式必须被删除后拒绝: %s", mode)
		}
	}
}

type fakeDynamicGoDriver struct {
	called bool
	value  protocolbus.EmbeddedPlugin
	err    error
}

func (*fakeDynamicGoDriver) Name() string              { return "dynamic-go" }
func (*fakeDynamicGoDriver) Isolation() IsolationLevel { return IsolationTrustedRuntime }
func (d *fakeDynamicGoDriver) Start(ctx context.Context, host *protocolbus.Host, _ InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error) {
	d.called = true
	if d.err != nil {
		return nil, d.err
	}
	return host.LaunchEmbeddedKindWithPolicy(ctx, d.value, policy, d.Name())
}

func dynamicPlugin() InstalledPlugin {
	return InstalledPlugin{
		ID: "cn.vastplan.foundation.test.dynamic", Publisher: "vastplan", Version: "1.0.0",
		Engines:       map[string]string{"backend": "^1.0"},
		DynamicGoPath: "/signed/content/plugin.so",
		Execution: pluginv1.BackendExecution{MinimumIsolation: string(IsolationTrustedProcess),
			DynamicGo: &pluginv1.DynamicGoExecution{Entry: "backend/plugin.so", ABI: protocolbus.DynamicGoABIV1,
				Fingerprint: strings.Repeat("a", 64)}},
		Contract: PluginRuntimeContract{Contributions: []pluginv1.RuntimeContribution{}},
	}
}

func TestRequireDynamicGoLoadsOnlyFirstPartySignedEntry(t *testing.T) {
	plugin := dynamicPlugin()
	driver := &fakeDynamicGoDriver{value: protocolbus.EmbeddedPlugin{ID: plugin.ID, Version: plugin.Version}}
	runtime := NewProtocolRuntime("1.0.0", nil)
	runtime.dynamicGoDriver = driver
	runtime.PlacementPolicy = PlacementPolicy{Default: PlacementRequireDynamicGo}
	runtime.ExecutionPolicy = ExecutionPolicy{DefaultPolicy: PublisherPolicyAllowTrusted}
	host := protocolbus.NewHost("backend", "1.0.0", registry.New(), nil)
	process, err := runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !driver.called || process.RuntimeKind() != "dynamic-go" {
		t.Fatalf("dynamic-go 驱动未被选择: called=%v process=%+v", driver.called, process)
	}

	plugin.ID, plugin.Publisher = "com.example.thirdparty", "example"
	driver.called = false
	_, err = runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
	})
	if err == nil || !strings.Contains(err.Error(), "publisher=vastplan") || driver.called {
		t.Fatalf("第三方插件不得进入 dynamic-go: called=%v err=%v", driver.called, err)
	}
}

func TestRequiredDynamicGoContractRejectsAnyNonDynamicPlacement(t *testing.T) {
	plugin := dynamicPlugin()
	plugin.Execution.DynamicGo.Required = true
	runtime := NewProtocolRuntime("1.0.0", nil)
	runtime.ExecutionPolicy = ExecutionPolicy{DefaultPolicy: PublisherPolicyAllowTrusted}
	host := protocolbus.NewHost("backend", "1.0.0", registry.New(), nil)
	for _, mode := range []PlacementMode{PlacementProcessOnly, PlacementPreferDynamicGo} {
		runtime.PlacementPolicy = PlacementPolicy{Default: mode}
		_, err := runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
			PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
		})
		if err == nil || !strings.Contains(err.Error(), "要求 require-dynamic-go") {
			t.Fatalf("required dynamic-go 不得降级为 %s: %v", mode, err)
		}
	}
}

func TestRequireDynamicGoRejectsHigherIsolationAndMissingModule(t *testing.T) {
	plugin := dynamicPlugin()
	plugin.Execution.MinimumIsolation = string(IsolationProcessSandbox)
	runtime := NewProtocolRuntime("1.0.0", nil)
	runtime.PlacementPolicy = PlacementPolicy{Default: PlacementRequireDynamicGo}
	runtime.ExecutionPolicy = ExecutionPolicy{DefaultPolicy: PublisherPolicyAllowTrusted}
	host := protocolbus.NewHost("backend", "1.0.0", registry.New(), nil)
	_, err := runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
	})
	if err == nil || !strings.Contains(err.Error(), "不能使用 dynamic-go trusted Runtime") {
		t.Fatalf("高隔离下限必须拒绝 dynamic-go: %v", err)
	}
	plugin.Execution.MinimumIsolation = string(IsolationTrustedProcess)
	plugin.DynamicGoPath = ""
	_, err = runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
	})
	if err == nil || !strings.Contains(err.Error(), "没有已验签的 dynamic-go 入口") {
		t.Fatalf("require-dynamic-go 不得回退进程: %v", err)
	}
}

func TestPreferDynamicGoFallsBackToProcessWhenModuleIsUnavailable(t *testing.T) {
	dynamicErr := errors.New("dynamic-go 当前平台不可用")
	plugin := dynamicPlugin()
	plugin.Execution.Driver = "native"
	driver := &fakeDynamicGoDriver{err: dynamicErr}
	runtime := NewProtocolRuntime("1.0.0", nil)
	runtime.dynamicGoDriver = driver
	runtime.PlacementPolicy = PlacementPolicy{Default: PlacementPreferDynamicGo}
	runtime.ExecutionPolicy = ExecutionPolicy{DefaultPolicy: PublisherPolicyAllowTrusted}
	host := protocolbus.NewHost("backend", "1.0.0", registry.New(), nil)
	_, err := runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
	})
	if !driver.called || err == nil || errors.Is(err, dynamicErr) {
		t.Fatalf("prefer-dynamic-go 应尝试模块后回退进程: called=%v err=%v", driver.called, err)
	}
}
