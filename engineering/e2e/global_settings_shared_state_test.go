//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/hostfactory"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestGlobalSettingsSharedStateSurvivesInstanceLoss(t *testing.T) {
	server := startE2ENATS(t)
	nc, err := nats.Connect(server.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	buckets, err := controlplane.EnsureBuckets(context.Background(), js, 1, jetstream.MemoryStorage)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sharedstate.NewNATSStore(buckets.SharedState)
	if err != nil {
		t.Fatal(err)
	}

	root := repoRoot(t)
	manifestRaw, err := os.ReadFile(filepath.Join(root, "extensions/plugins/cn.vastplan.platform.configuration.global-settings/vastplan.plugin.json"))
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
	bin := buildPlugin(t, "./extensions/plugins/cn.vastplan.platform.configuration.global-settings/backend")
	firstHost, first := launchGlobalSettingsHost(t, store, bin, contributions, "node-a", "instance-a")
	secondHost, second := launchGlobalSettingsHost(t, store, bin, contributions, "node-b", "instance-b")
	defer func() { _ = secondHost.Close(second) }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	created, err := firstHost.Invoke(ctx, toolTarget("platform.settings", "put"), testCallContext(), []byte(`{"key":"system.theme","value":"dark","ifVersion":0}`))
	if err != nil || created.Result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("第一实例写入失败: response=%+v err=%v", created, err)
	}
	if err := firstHost.Close(first); err != nil {
		t.Fatal(err)
	}
	loaded, err := secondHost.Invoke(ctx, toolTarget("platform.settings", "get"), testCallContext(), []byte(`{"key":"system.theme"}`))
	if err != nil || loaded.Result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("第二实例接管读取失败: response=%+v err=%v", loaded, err)
	}
	var value map[string]any
	if err := json.Unmarshal(loaded.Payload, &value); err != nil || value["value"] != "dark" || value["version"] != float64(1) {
		t.Fatalf("共享状态内容错误: %s err=%v", loaded.Payload, err)
	}
}

func launchGlobalSettingsHost(t *testing.T, store sharedstate.Store, bin string, contributions []pluginv1.RuntimeContribution, nodeID, instanceID string) (*protocolbus.Host, *protocolbus.PluginInstance) {
	t.Helper()
	host, err := hostfactory.NewWithDependencies("0.1.0", t.Logf, kernelspi.Dependencies{SharedState: store})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Stop)
	allowAllPermissions(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	instance, err := host.LaunchWithPolicy(ctx, bin, protocolbus.LaunchPolicy{
		PluginID: "cn.vastplan.platform.configuration.global-settings", Publisher: "vastplan", Version: "0.8.1",
		ArtifactSHA256: strings.Repeat("a", 64), NodeID: nodeID, RuntimeScope: "platform-settings", RuntimeInstanceID: instanceID,
		Contributions:  contributions,
		KernelServices: []string{"kernel.state.shared.get", "kernel.state.shared.create", "kernel.state.shared.update"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return host, instance
}
