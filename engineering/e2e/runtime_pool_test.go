//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

func TestRuntimePool_NodeWorkersShareOneProcessAndReleaseIndependently(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("未安装 Node.js")
	}
	root := repoRoot(t)
	sdkURL := "file://" + filepath.Join(root, "extensions/sdk/node/backend-plugin/src/index.mjs")
	writePlugin := func(id, capability string) string {
		path := filepath.Join(t.TempDir(), "main.mjs")
		source := fmt.Sprintf(`
import { Contribution, Plugin, callResult } from %q;
const plugin = new Plugin({ id: %q, version: '1.0.0', engines: { backend: '^1.0' } });
plugin.contribute(new Contribution({
  extensionPoint: 'tool.package', id: %q, descriptor: { title: %q },
  handlers: { echo: (_invocation, _host, _context, payload) => callResult.ok(payload) },
}));
export const start = () => plugin.serve();
export const shutdown = () => plugin.shutdown();
`, sdkURL, id, capability, capability)
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	host := newHost(t, "1.0.0")
	allowAllPermissions(t, host)
	pools := nodeagent.NewRuntimePoolManager(t.Logf)
	t.Cleanup(func() { _ = pools.Close() })
	driver := nodeagent.NodeWorkerExecutionDriver{
		Command: node, HostArgs: []string{filepath.Join(root, "core/runtimehosts/node-worker/host.mjs")},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := func(id, capability, generation string) *protocolbus.PluginInstance {
		entry := writePlugin(id, capability)
		plugin := nodeagent.InstalledPlugin{
			ID: id, Publisher: "vastplan", Version: "1.0.0", Root: filepath.Dir(entry), EntryPath: entry,
			Execution: pluginv1.BackendExecution{
				Driver: "node-worker", MinimumIsolation: "trusted-runtime",
				Requirements: map[string]string{"node": ">=20"},
				Node:         &pluginv1.NodeExecution{WorkerSafe: true, ModuleFormat: "esm"},
			},
		}
		instance, err := driver.StartManaged(ctx, host, plugin, protocolbus.LaunchPolicy{
			PluginID: id, Publisher: "vastplan", Version: "1.0.0", RuntimeScope: "backend-main",
			RuntimeGeneration: generation,
			Contributions: []pluginv1.RuntimeContribution{{
				ExtensionPoint: "tool.package", ID: capability,
				Descriptor: []byte(fmt.Sprintf(`{"title":%q}`, capability)),
			}},
		}, pools, nodeagent.RuntimeHostingPolicy{Default: nodeagent.RuntimeHostingShared})
		if err != nil {
			t.Fatal(err)
		}
		return instance
	}

	first := start("cn.vastplan.test.pool.first", "vastplan.pool.first", "generation-1")
	second := start("cn.vastplan.test.pool.second", "vastplan.pool.second", "generation-1")
	if first.PID <= 0 || first.PID != second.PID {
		t.Fatalf("共享池应返回同一个物理 PID: first=%d second=%d", first.PID, second.PID)
	}
	if snapshot := pools.Snapshot(); len(snapshot) != 1 || snapshot[0].Units != 2 || !snapshot[0].Healthy {
		t.Fatalf("共享池状态异常: %+v", snapshot)
	}
	candidate := start("cn.vastplan.test.pool.candidate", "vastplan.pool.candidate", "generation-2")
	if candidate.PID <= 0 || candidate.PID == first.PID {
		t.Fatalf("候选 generation 必须使用独立物理 Host: active=%d candidate=%d", first.PID, candidate.PID)
	}
	if snapshot := pools.Snapshot(); len(snapshot) != 2 {
		t.Fatalf("活动代与候选代重叠时应存在两个 Runtime Pool: %+v", snapshot)
	}
	if err := host.Close(candidate); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(first); err != nil {
		t.Fatal(err)
	}
	if snapshot := pools.Snapshot(); len(snapshot) != 1 || snapshot[0].Units != 1 {
		t.Fatalf("关闭单个逻辑单元不应回收共享 Host: %+v", snapshot)
	}
	response, err := host.Invoke(ctx, toolTarget("vastplan.pool.second", "echo"), testCallContext(), []byte("still-alive"))
	if err != nil || response.Result.GetStatus() != contractv1.CallResult_STATUS_OK ||
		strings.TrimSpace(string(response.Payload)) != "still-alive" {
		t.Fatalf("同池其他插件应继续可用: response=%+v err=%v", response, err)
	}
	if err := host.Close(second); err != nil {
		t.Fatal(err)
	}
	if snapshot := pools.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("最后一个执行单元退出后应回收空池: %+v", snapshot)
	}
}

func TestRuntimePool_PythonSubinterpretersShareOneProcess(t *testing.T) {
	pythonCommand := os.Getenv("VASTPLAN_TEST_PYTHON")
	if pythonCommand == "" {
		pythonCommand = "python3"
	}
	python, err := exec.LookPath(pythonCommand)
	if err != nil {
		t.Skip("未安装 Python")
	}
	root := repoRoot(t)
	hostScript := filepath.Join(root, "core/runtimehosts/python-subinterpreter/host.py")
	probe := exec.Command(python, hostScript, "--probe")
	if output, err := probe.CombinedOutput(); err != nil || !strings.Contains(string(output), `"supported": true`) {
		t.Skipf("Python Runtime Host 不支持子解释器: %v %s", err, output)
	}

	writePlugin := func(id, capability string) string {
		path := filepath.Join(t.TempDir(), "main.py")
		source := fmt.Sprintf(`
from vastplan_subinterpreter import Contribution, Plugin, call_ok
plugin = Plugin(%q, "1.0.0", {"backend": "^1.0"})
plugin.contribute(Contribution(
    extension_point="tool.package", id=%q, descriptor=b'{"title": %q}',
    handlers={"echo": lambda _invocation, _host, _context, payload: call_ok(payload)},
))
plugin.serve()
`, id, capability, capability)
		if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	host := newHost(t, "1.0.0")
	allowAllPermissions(t, host)
	pools := nodeagent.NewRuntimePoolManager(t.Logf)
	t.Cleanup(func() { _ = pools.Close() })
	driver := nodeagent.PythonSubinterpreterExecutionDriver{Command: python, HostArgs: []string{hostScript}}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	start := func(id, capability string) *protocolbus.PluginInstance {
		entry := writePlugin(id, capability)
		plugin := nodeagent.InstalledPlugin{
			ID: id, Publisher: "vastplan", Version: "1.0.0", Root: filepath.Dir(entry), EntryPath: entry,
			Execution: pluginv1.BackendExecution{
				Driver: "python-subinterpreter", MinimumIsolation: "trusted-runtime",
				Requirements: map[string]string{"python": ">=3.14"},
				Python:       &pluginv1.PythonExecution{SubinterpreterSafe: true},
			},
		}
		instance, err := driver.StartManaged(ctx, host, plugin, protocolbus.LaunchPolicy{
			PluginID: id, Publisher: "vastplan", Version: "1.0.0", RuntimeScope: "backend-main",
			RuntimeGeneration: "generation-1",
			Contributions: []pluginv1.RuntimeContribution{{
				ExtensionPoint: "tool.package", ID: capability,
				Descriptor: []byte(fmt.Sprintf(`{"title":%q}`, capability)),
			}},
		}, pools, nodeagent.RuntimeHostingPolicy{Default: nodeagent.RuntimeHostingShared})
		if err != nil {
			t.Fatal(err)
		}
		return instance
	}

	first := start("cn.vastplan.test.python-pool.first", "vastplan.python-pool.first")
	second := start("cn.vastplan.test.python-pool.second", "vastplan.python-pool.second")
	if first.PID <= 0 || first.PID != second.PID {
		t.Fatalf("Python 子解释器应共享物理 PID: first=%d second=%d", first.PID, second.PID)
	}
	if snapshot := pools.Snapshot(); len(snapshot) != 1 || snapshot[0].Units != 2 {
		t.Fatalf("Python Runtime Pool 状态异常: %+v", snapshot)
	}
	if err := host.Close(first); err != nil {
		t.Fatal(err)
	}
	response, err := host.Invoke(ctx, toolTarget("vastplan.python-pool.second", "echo"), testCallContext(), []byte("still-alive"))
	if err != nil || response.Result.GetStatus() != contractv1.CallResult_STATUS_OK || string(response.Payload) != "still-alive" {
		t.Fatalf("关闭一个子解释器后同池插件应继续可用: response=%+v err=%v", response, err)
	}
	if err := host.Close(second); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimePool_DynamicGoLoadsOutsideBackendProcess(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "freebsd" {
		t.Skip("当前平台不支持 Go plugin")
	}
	const fingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	root := repoRoot(t)
	buildRoot := t.TempDir()
	hostBinary := filepath.Join(buildRoot, "vastplan-go-dynamic-host")
	moduleBinary := filepath.Join(buildRoot, "bootstrap-policy.so")
	build := func(arguments ...string) {
		command := exec.Command("go", arguments...)
		command.Dir = root
		command.Env = append(os.Environ(), "CGO_ENABLED=1", "GOCACHE="+filepath.Join(buildRoot, "go-cache"))
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("构建 dynamic-go E2E 制品: %v\n%s", err, output)
		}
	}
	build("build", "-trimpath", "-buildvcs=false",
		"-ldflags", "-X main.dynamicGoHostFingerprint="+fingerprint,
		"-o", hostBinary, "./core/runtimehosts/go-dynamic")
	build("build", "-trimpath", "-buildvcs=false", "-buildmode=plugin",
		"-ldflags", "-X main.dynamicGoBuildFingerprint="+fingerprint,
		"-o", moduleBinary,
		"./extensions/plugins/cn.vastplan.foundation.security.bootstrap-policy/dynamic")

	manifestRaw, err := os.ReadFile(filepath.Join(root,
		"extensions/plugins/cn.vastplan.foundation.security.bootstrap-policy/vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}

	host := newHost(t, "0.1.0")
	pools := nodeagent.NewRuntimePoolManager(t.Logf)
	t.Cleanup(func() { _ = pools.Close() })
	driver := nodeagent.DynamicGoExecutionDriver{Command: hostBinary}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	instance, err := driver.StartManaged(ctx, host, nodeagent.InstalledPlugin{
		ID: manifest.ID, Publisher: manifest.Publisher, Version: manifest.Version,
		Engines: manifest.Engines, Root: buildRoot, DynamicGoPath: moduleBinary,
		SHA256: strings.Repeat("b", 64),
		Execution: pluginv1.BackendExecution{
			MinimumIsolation: "trusted-runtime",
			DynamicGo: &pluginv1.DynamicGoExecution{
				Entry: "bootstrap-policy.so", ABI: protocolbus.DynamicGoABIV1, Fingerprint: fingerprint,
			},
		},
	}, protocolbus.LaunchPolicy{
		PluginID: manifest.ID, Publisher: manifest.Publisher, Version: manifest.Version,
		Contributions: contributions, RuntimeScope: "backend-main", RuntimeGeneration: "generation-1",
	}, pools, nodeagent.RuntimeHostingPolicy{Default: nodeagent.RuntimeHostingShared})
	if err != nil {
		t.Fatal(err)
	}
	if instance.RuntimeKind() != "dynamic-go" || instance.PID <= 0 || instance.PID == os.Getpid() {
		t.Fatalf("dynamic-go 必须在独立 Go Runtime Host 中运行: kind=%s pid=%d backend=%d",
			instance.RuntimeKind(), instance.PID, os.Getpid())
	}
	if snapshot := pools.Snapshot(); len(snapshot) != 1 || snapshot[0].PID != instance.PID || snapshot[0].Units != 1 {
		t.Fatalf("Go Runtime Pool 状态异常: %+v", snapshot)
	}
	if err := host.Close(instance); err != nil {
		t.Fatal(err)
	}
}
