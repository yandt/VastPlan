package nodeagent

import (
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
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
	runtime.ExecutionPolicy = NewExecutionPolicy([]string{"vastplan"}, true)
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
