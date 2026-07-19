package nodeagent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestIsolationDriversAreRegisteredOnlyFromOperatorConfiguration(t *testing.T) {
	t.Setenv(processSandboxHostEnv, "")
	t.Setenv(containerHostEnv, "")
	t.Setenv(wasmComponentHostEnv, "")
	registry := DefaultExecutionDrivers()
	for _, name := range []string{"process-sandbox", "container", "wasm-component"} {
		if _, ok := registry.Resolve(name); ok {
			t.Fatalf("未配置可信 Runtime Host 时不应注册 %s", name)
		}
	}

	t.Setenv(processSandboxHostEnv, "/kernel/runtimehosts/process-sandbox")
	t.Setenv(containerHostEnv, "/kernel/runtimehosts/container")
	t.Setenv(wasmComponentHostEnv, "/kernel/runtimehosts/wasm-component")
	registry = DefaultExecutionDrivers()
	want := map[string]IsolationLevel{
		"process-sandbox": IsolationProcessSandbox,
		"container":       IsolationContainer,
		"wasm-component":  IsolationWASM,
	}
	for name, level := range want {
		driver, ok := registry.Resolve(name)
		if !ok || driver.Isolation() != level {
			t.Fatalf("隔离驱动注册异常 %s: driver=%v", name, driver)
		}
	}
}

func TestIsolationDriverBuildsNoShellTrustedLaunchSpec(t *testing.T) {
	launcher := filepath.Join(t.TempDir(), "sandbox-host")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	entry := filepath.Join(root, "plugin")
	if err := os.WriteFile(entry, []byte("plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := IsolationExecutionDriver{
		DriverName: "process-sandbox", Level: IsolationProcessSandbox, HostCommand: launcher,
	}
	spec, err := driver.launchSpec(InstalledPlugin{
		ID: "cn.example.third-party", Root: root, EntryPath: entry,
		Execution: pluginv1.BackendExecution{Driver: "process-sandbox", Args: []string{"--value", "$(touch /tmp/never)"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != launcher || spec.RuntimeKind != "process-sandbox" {
		t.Fatalf("隔离启动规格异常: %+v", spec)
	}
	if !slices.Contains(spec.Args, "--plugin-root") || !slices.Contains(spec.Args, entry) ||
		!slices.Contains(spec.Args, "$(touch /tmp/never)") {
		t.Fatalf("参数必须原样通过 argv 传递: %v", spec.Args)
	}
}

func TestIsolationDriverRejectsEntryEscapeAndUntrustedHostPath(t *testing.T) {
	driver := IsolationExecutionDriver{
		DriverName: "process-sandbox", Level: IsolationProcessSandbox, HostCommand: "definitely-not-installed",
	}
	plugin := InstalledPlugin{ID: "p", Root: "/plugins/p", EntryPath: "/plugins/p/main"}
	if _, err := driver.launchSpec(plugin); err == nil || !strings.Contains(err.Error(), "绝对路径") {
		t.Fatalf("无法解析的相对 Host 必须拒绝: %v", err)
	}

	launcher := filepath.Join(t.TempDir(), "sandbox-host")
	if err := os.WriteFile(launcher, []byte("host"), 0o700); err != nil {
		t.Fatal(err)
	}
	driver.HostCommand = launcher
	plugin.Root = t.TempDir()
	plugin.EntryPath = filepath.Join(filepath.Dir(plugin.Root), "escaped")
	if _, err := driver.launchSpec(plugin); err == nil || !strings.Contains(err.Error(), "逃逸") {
		t.Fatalf("入口逃逸必须拒绝: %v", err)
	}
}
