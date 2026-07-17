package nodeagent

import (
	"context"
	"errors"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/shared/go/registry"
)

func TestPlacementPolicyPrecedence(t *testing.T) {
	policy, err := ParsePlacementPolicy("process-only", "vastplan=prefer-embedded",
		"com.vastplan.foundation.security.bootstrap-policy=require-embedded")
	if err != nil {
		t.Fatal(err)
	}
	plugin := InstalledPlugin{ID: "com.vastplan.foundation.security.bootstrap-policy", Publisher: "vastplan"}
	if got := policy.modeFor(plugin); got != PlacementRequireEmbedded {
		t.Fatalf("插件级规则应优先，实际 %s", got)
	}
	plugin.ID = "com.vastplan.platform.settings"
	if got := policy.modeFor(plugin); got != PlacementPreferEmbedded {
		t.Fatalf("发布者级规则应次优先，实际 %s", got)
	}
	plugin.Publisher = "other"
	if got := policy.modeFor(plugin); got != PlacementProcessOnly {
		t.Fatalf("应回退全局规则，实际 %s", got)
	}
}

func TestRequireEmbeddedRejectsHigherIsolationAndMissingCatalog(t *testing.T) {
	catalog, err := NewEmbeddedCatalog(func() protocolbus.EmbeddedPlugin {
		return protocolbus.EmbeddedPlugin{ID: "com.vastplan.foundation.test.module", Version: "1.0.0"}
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewProtocolRuntime("1.0.0", nil)
	runtime.EmbeddedCatalog = catalog
	runtime.PlacementPolicy = PlacementPolicy{Default: PlacementRequireEmbedded}
	runtime.ExecutionPolicy = ExecutionPolicy{DefaultPolicy: PublisherPolicyAllowTrusted}
	host := protocolbus.NewHost("backend", "1.0.0", registry.New(), nil)
	plugin := InstalledPlugin{ID: "com.vastplan.foundation.test.module", Version: "1.0.0", Publisher: "vastplan"}
	plugin.Execution.MinimumIsolation = string(IsolationProcessSandbox)
	_, err = runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
	})
	if err == nil || !strings.Contains(err.Error(), "不能放入内核进程") {
		t.Fatalf("高隔离下限必须拒绝内嵌: %v", err)
	}
	plugin.ID = "com.vastplan.foundation.test.missing"
	plugin.Execution.MinimumIsolation = string(IsolationTrustedProcess)
	_, err = runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
	})
	if err == nil || !strings.Contains(err.Error(), "既不在静态目录") {
		t.Fatalf("require-embedded 不得回退进程: %v", err)
	}
}

type fakeDynamicGoLoader struct {
	called bool
	value  protocolbus.EmbeddedPlugin
	err    error
}

func (l *fakeDynamicGoLoader) Load(_, _, _, _ string) (protocolbus.EmbeddedPlugin, error) {
	l.called = true
	return l.value, l.err
}

func TestRequireDynamicGoLoadsOnlyFirstPartySignedEntry(t *testing.T) {
	plugin := InstalledPlugin{
		ID: "com.vastplan.foundation.test.dynamic", Publisher: "vastplan", Version: "1.0.0",
		DynamicGoPath: "/signed/content/plugin.so",
		Execution: pluginv1.BackendExecution{
			MinimumIsolation: string(IsolationTrustedProcess),
			DynamicGo: &pluginv1.DynamicGoExecution{Entry: "backend/plugin.so", ABI: protocolbus.DynamicGoABIV1,
				Fingerprint: strings.Repeat("a", 64)},
		},
		Contract: PluginRuntimeContract{Contributions: []pluginv1.RuntimeContribution{}},
	}
	loader := &fakeDynamicGoLoader{value: protocolbus.EmbeddedPlugin{ID: plugin.ID, Version: plugin.Version}}
	runtime := NewProtocolRuntime("1.0.0", nil)
	runtime.DynamicGoLoader = loader
	runtime.PlacementPolicy = PlacementPolicy{Default: PlacementRequireDynamicGo}
	runtime.ExecutionPolicy = ExecutionPolicy{DefaultPolicy: PublisherPolicyAllowTrusted}
	host := protocolbus.NewHost("backend", "1.0.0", registry.New(), nil)
	process, err := runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !loader.called || process.RuntimeKind() != "embedded" {
		t.Fatalf("dynamic-go 未以内嵌实例启动: called=%v process=%+v", loader.called, process)
	}

	plugin.ID, plugin.Publisher = "com.example.thirdparty", "example"
	loader.called = false
	_, err = runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
	})
	if err == nil || !strings.Contains(err.Error(), "publisher=vastplan") || loader.called {
		t.Fatalf("第三方插件不得进入 dynamic-go: called=%v err=%v", loader.called, err)
	}
}

func TestPreferDynamicGoFallsBackToProcessWhenModuleIsUnavailable(t *testing.T) {
	dynamicErr := errors.New("dynamic-go 当前平台不可用")
	plugin := InstalledPlugin{
		ID: "com.vastplan.foundation.test.dynamic", Publisher: "vastplan", Version: "1.0.0",
		DynamicGoPath: "/signed/content/plugin.so",
		Execution: pluginv1.BackendExecution{Driver: "native", MinimumIsolation: string(IsolationTrustedProcess),
			DynamicGo: &pluginv1.DynamicGoExecution{Entry: "backend/plugin.so", ABI: protocolbus.DynamicGoABIV1,
				Fingerprint: strings.Repeat("a", 64)}},
	}
	loader := &fakeDynamicGoLoader{err: dynamicErr}
	runtime := NewProtocolRuntime("1.0.0", nil)
	runtime.DynamicGoLoader = loader
	runtime.PlacementPolicy = PlacementPolicy{Default: PlacementPreferDynamicGo}
	runtime.ExecutionPolicy = ExecutionPolicy{DefaultPolicy: PublisherPolicyAllowTrusted}
	host := protocolbus.NewHost("backend", "1.0.0", registry.New(), nil)
	_, err := runtime.startPlugin(context.Background(), host, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Version: plugin.Version, Contributions: []pluginv1.RuntimeContribution{},
	})
	if !loader.called || err == nil || errors.Is(err, dynamicErr) {
		t.Fatalf("prefer-dynamic-go 应尝试模块后回退进程: called=%v err=%v", loader.called, err)
	}
}

func TestEmbeddedCatalogRequiresExactVersionAndStableIdentity(t *testing.T) {
	catalog, err := NewEmbeddedCatalog(func() protocolbus.EmbeddedPlugin {
		return protocolbus.EmbeddedPlugin{ID: "com.vastplan.test", Version: "1.0.0"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := catalog.Resolve("com.vastplan.test", "1.0.1"); err != nil || found {
		t.Fatalf("静态目录不得跨版本匹配: found=%v err=%v", found, err)
	}
	if _, found, err := catalog.Resolve("com.vastplan.test", "1.0.0"); err != nil || !found {
		t.Fatalf("精确版本应命中: found=%v err=%v", found, err)
	}
}

func TestParsePlacementPolicyRejectsInvalidRules(t *testing.T) {
	valid, err := ParsePlacementPolicy("process-only", "vastplan=prefer-dynamic-go",
		"com.vastplan.foundation.test.dynamic=require-dynamic-go")
	if err != nil || valid.PublisherPolicies["vastplan"] != PlacementPreferDynamicGo ||
		valid.PluginPolicies["com.vastplan.foundation.test.dynamic"] != PlacementRequireDynamicGo {
		t.Fatalf("dynamic-go 放置策略应可解析: %+v err=%v", valid, err)
	}
	for _, input := range []struct{ defaultMode, publishers, plugins string }{
		{"embedded", "", ""},
		{"process-only", "vastplan", ""},
		{"process-only", "", "one=prefer-embedded,one=require-embedded"},
	} {
		if _, err := ParsePlacementPolicy(input.defaultMode, input.publishers, input.plugins); err == nil {
			t.Fatalf("应拒绝无效放置策略: %+v", input)
		}
	}
}
