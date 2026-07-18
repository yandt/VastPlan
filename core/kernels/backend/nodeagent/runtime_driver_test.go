package nodeagent

import (
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type sandboxTestDriver struct{}

func (sandboxTestDriver) Name() string              { return "sandbox.test" }
func (sandboxTestDriver) Isolation() IsolationLevel { return IsolationProcessSandbox }
func (sandboxTestDriver) LaunchSpec(plugin InstalledPlugin) (protocolbus.LaunchSpec, error) {
	return protocolbus.LaunchSpec{Command: "sandbox", Args: []string{plugin.EntryPath}}, nil
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
	spec, err := runtime.launchSpec(plugin)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != "python3" || len(spec.Args) != 2 || spec.Args[0] != plugin.EntryPath || spec.Dir != plugin.Root {
		t.Fatalf("python 驱动启动规格错误: %+v", spec)
	}

	plugin.Publisher = "third-party"
	if _, err := runtime.launchSpec(plugin); err == nil || !strings.Contains(err.Error(), "要求隔离") {
		t.Fatalf("未知发布者不能降级为 trusted-process: %v", err)
	}

	registry, err := NewRuntimeDriverRegistry(sandboxTestDriver{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.Drivers = registry
	plugin.Execution.Driver = "sandbox.test"
	if _, err := runtime.launchSpec(plugin); err != nil {
		t.Fatalf("隔离驱动应允许第三方发布者: %v", err)
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

func TestRuntimeDriversRejectUnknownDriverAndUnsupportedPlatform(t *testing.T) {
	runtime := NewProtocolRuntime("1.0.0", nil)
	plugin := InstalledPlugin{ID: "p", Publisher: "vastplan", EntryPath: "/p", Execution: pluginv1.BackendExecution{
		Driver: "missing", MinimumIsolation: "trusted-process",
	}}
	if _, err := runtime.launchSpec(plugin); err == nil || !strings.Contains(err.Error(), "未注册运行驱动") {
		t.Fatalf("未知驱动必须 fail-closed: %v", err)
	}
	plugin.Execution.Driver = "native"
	plugin.Execution.Platforms = []string{"unsupported/none"}
	if _, err := runtime.launchSpec(plugin); err == nil || !strings.Contains(err.Error(), "不支持当前平台") {
		t.Fatalf("平台不匹配必须 fail-closed: %v", err)
	}
}
