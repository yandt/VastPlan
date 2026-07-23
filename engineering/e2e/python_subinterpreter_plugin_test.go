//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

func TestPythonSubinterpreterPlugin_CrossInterpreterInvoke(t *testing.T) {
	pythonCommand := os.Getenv("VASTPLAN_TEST_PYTHON")
	if pythonCommand == "" {
		pythonCommand = "python3"
	}
	python, err := exec.LookPath(pythonCommand)
	if err != nil {
		t.Skip("未安装 python3")
	}
	root := repoRoot(t)
	hostScript := filepath.Join(root, "core/runtimehosts/python-subinterpreter/host.py")
	probe := exec.Command(python, hostScript, "--probe")
	probeOutput, err := probe.Output()
	if err != nil {
		t.Skipf("Python Runtime Host 探测失败: %v", err)
	}
	var capability struct {
		Supported bool `json:"supported"`
	}
	if err := json.Unmarshal(probeOutput, &capability); err != nil {
		t.Fatal(err)
	}
	if !capability.Supported {
		t.Skip("当前解释器不是 CPython 3.14+，只验证 fail-closed 探测")
	}
	dependencyProbe := exec.Command(python, "-c", "import grpc, google.protobuf")
	dependencyProbe.Env = []string{"PYTHONPATH=" + filepath.Join(root, "extensions/sdk/python")}
	if output, err := dependencyProbe.CombinedOutput(); err != nil {
		t.Skipf("Python gRPC 依赖不可用: %v %s", err, output)
	}

	manifestPath := filepath.Join(root, "extensions/plugins/cn.vastplan.foundation.backend.runtime.python-subinterpreter-hello/vastplan.plugin.json")
	manifestRaw, err := os.ReadFile(manifestPath)
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

	host := newHost(t, "1.0.0")
	allowAllPermissions(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	instance, err := host.LaunchSpecWithPolicy(ctx, protocolbus.LaunchSpec{
		Command: python,
		Args: []string{
			hostScript,
			"--entry", filepath.Join(root, "extensions/plugins/cn.vastplan.foundation.backend.runtime.python-subinterpreter-hello/backend/main.py"),
		},
		Dir: root,
		ExtraEnv: []string{
			"PYTHONPATH=" + filepath.Join(root, "extensions/sdk/python"),
		},
		RuntimeKind: "python-subinterpreter",
	}, protocolbus.LaunchPolicy{
		PluginID:  "cn.vastplan.foundation.backend.runtime.python-subinterpreter-hello",
		Publisher: "vastplan", Version: "0.2.0", Contributions: contributions,
		KernelServices: []string{"kernel.info"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Close(instance) }()

	response, err := host.Invoke(ctx, toolTarget("vastplan.python-subinterpreter-hello", "echo"), testCallContext(), []byte(`{"text":"子解释器成功"}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.Result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("Python 子解释器插件调用失败: %+v", response.Result)
	}
	var output map[string]any
	if err := json.Unmarshal(response.Payload, &output); err != nil {
		t.Fatal(err)
	}
	if output["echo"] != "子解释器成功" || output["runtime"] != "python-subinterpreter" {
		t.Fatalf("子解释器响应异常: %v", output)
	}

	response, err = host.Invoke(ctx, toolTarget("vastplan.python-subinterpreter-hello", "whoami"), testCallContext(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.Result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("Python 子解释器 HostCall 失败: %+v", response.Result)
	}
	if err := json.Unmarshal(response.Payload, &output); err != nil {
		t.Fatal(err)
	}
	if output["callerKind"] != "CALLER_KIND_PLUGIN" ||
		output["callerId"] != "cn.vastplan.foundation.backend.runtime.python-subinterpreter-hello" ||
		output["tenant"] != "acme" {
		t.Fatalf("子解释器 HostCall 信任边界错误: %v", output)
	}
}
