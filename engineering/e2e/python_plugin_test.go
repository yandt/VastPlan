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
	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

func TestPythonPlugin_CrossLanguageInvokeAndFeatureNegotiation(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("未安装 python3")
	}
	root := repoRoot(t)
	probe := exec.Command(python, "-c", "import grpc, google.protobuf")
	probe.Env = []string{"PYTHONPATH=" + filepath.Join(root, "extensions/sdk/python")}
	if output, err := probe.CombinedOutput(); err != nil {
		t.Skipf("Python gRPC 依赖不可用: %v %s", err, output)
	}

	host := newHost(t, "1.0.0")
	allowAllPermissions(t, host)
	manifestRaw, err := os.ReadFile(filepath.Join(root, "extensions/plugins/cn.vastplan.python-hello/vastplan.plugin.json"))
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	process, err := host.LaunchSpecWithPolicy(ctx, protocolbus.LaunchSpec{
		Command: python,
		Args:    []string{filepath.Join(root, "extensions/plugins/cn.vastplan.python-hello/backend/main.py")},
		Dir:     root,
		ExtraEnv: []string{
			"PYTHONPATH=" + filepath.Join(root, "extensions/sdk/python"),
		},
	}, protocolbus.LaunchPolicy{
		PluginID: "cn.vastplan.python-hello", Version: "0.2.0", Contributions: contributions,
		RequiredFeatures: []string{protocol.FeatureCancellation, protocol.FeatureEventPublish},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Close(process) }()

	response, err := host.Invoke(ctx, toolTarget("vastplan.python-hello", "echo"), testCallContext(), []byte(`{"text":"异构成功"}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.Result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("Python 插件调用失败: %+v", response.Result)
	}
	var output map[string]any
	if err := json.Unmarshal(response.Payload, &output); err != nil {
		t.Fatal(err)
	}
	if output["echo"] != "异构成功" || output["runtime"] != "python" {
		t.Fatalf("跨语言响应异常: %v", output)
	}
}
