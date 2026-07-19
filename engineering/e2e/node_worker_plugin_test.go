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

func TestNodeWorkerPlugin_CrossLanguageInvokeAndLifecycle(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("未安装 Node.js")
	}
	root := repoRoot(t)
	manifestPath := filepath.Join(root, "extensions/plugins/cn.vastplan.foundation.backend.runtime.node-worker-hello/vastplan.plugin.json")
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
		Command: node,
		Args: []string{
			filepath.Join(root, "core/runtimehosts/node-worker/host.mjs"),
			"--entry", filepath.Join(root, "extensions/plugins/cn.vastplan.foundation.backend.runtime.node-worker-hello/backend/main.mjs"),
		},
		Dir:         root,
		RuntimeKind: "node-worker",
	}, protocolbus.LaunchPolicy{
		PluginID: "cn.vastplan.foundation.backend.runtime.node-worker-hello", Publisher: "vastplan", Version: "0.1.0",
		Contributions: contributions, RequiredFeatures: []string{protocol.FeatureCancellation},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Close(instance) }()
	if instance.RuntimeKind() != "node-worker" {
		t.Fatalf("运行形态异常: %s", instance.RuntimeKind())
	}

	response, err := host.Invoke(ctx, toolTarget("vastplan.node-worker-hello", "echo"), testCallContext(), []byte(`{"text":"Worker 成功"}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.Result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("Node Worker 插件调用失败: %+v", response.Result)
	}
	var output map[string]any
	if err := json.Unmarshal(response.Payload, &output); err != nil {
		t.Fatal(err)
	}
	if output["echo"] != "Worker 成功" || output["runtime"] != "node-worker" {
		t.Fatalf("跨语言响应异常: %v", output)
	}
}
