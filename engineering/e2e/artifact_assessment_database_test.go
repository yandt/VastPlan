//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	provider "cdsoft.com.cn/VastPlan/extensions/sdk/go/artifactassessmentprovider"
)

func TestArtifactAssessmentDatabaseFileRealProcessMaterializesBeforeReady(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "staging")
	if err := os.MkdirAll(filepath.Join(staging, "db"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"metadata.json": []byte(`{"Version":2}`), "trivy.db": []byte("e2e database")} {
		if err := os.WriteFile(filepath.Join(staging, "db", name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	revision, err := provider.TrivyDatabaseRevision(staging)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRoot := filepath.Join(root, "snapshots-root")
	configuration, _ := json.Marshal(map[string]any{"sourceDirectory": staging, "snapshotRoot": snapshotRoot, "databaseRevision": revision})
	manifestRaw, err := os.ReadFile(filepath.Join(repoRoot(t), "extensions/plugins/cn.vastplan.platform.artifacts.assessment.database.file/vastplan.plugin.json"))
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
	allowAllPermissions(t, host)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	instance, err := host.LaunchWithPolicy(ctx, buildPlugin(t, "./extensions/plugins/cn.vastplan.platform.artifacts.assessment.database.file/backend"), protocolbus.LaunchPolicy{
		PluginID: manifest.ID, Publisher: manifest.Publisher, Version: manifest.Version,
		Contributions: contributions, ContextAccess: pluginv1.ContextAccessContract(manifest), Configuration: configuration,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = host.Close(instance) }()
	response, err := host.Invoke(ctx, toolTarget("platform.artifacts.assessment.database.file", "status"), testCallContext(), nil)
	if err != nil || response.GetResult().GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("读取真实 Snapshot 插件状态失败: response=%+v err=%v", response, err)
	}
	var status struct {
		Ready            bool   `json:"ready"`
		DatabaseRevision string `json:"databaseRevision"`
		Files            int    `json:"files"`
	}
	if err := json.Unmarshal(response.Payload, &status); err != nil || !status.Ready || status.DatabaseRevision != revision || status.Files != 2 {
		t.Fatalf("真实 Snapshot 插件在 ready 前未完成物化: status=%+v err=%v", status, err)
	}
	materialized := filepath.Join(snapshotRoot, "snapshots", revision)
	if got, err := provider.TrivyDatabaseRevision(materialized); err != nil || got != revision {
		t.Fatalf("真实进程物化字节无效: got=%s err=%v", got, err)
	}
}
