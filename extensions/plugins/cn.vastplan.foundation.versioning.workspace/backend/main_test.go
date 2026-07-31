package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspace "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.workspace/versionworkspace"
)

func TestPluginVersionMatchesManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != pluginVersion || pluginVersion != workspace.PluginVersion {
		t.Fatalf("manifest=%q main=%q package=%q", manifest.Version, pluginVersion, workspace.PluginVersion)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(contributions) != 1 {
		t.Fatalf("manifest contribution 无效: %+v %v", contributions, err)
	}
	service, err := workspace.BuildConfiguredService(workspace.StartupConfiguration{Environments: []resourcev1.EnvironmentProfile{{
		Protocol: resourcev1.Protocol, ID: "test", Revision: 1,
		Bindings: []resourcev1.ResourceBinding{{ResourceType: "portal.configuration", Namespace: "portal.configuration", Adapter: workspace.JSONAdapterID, AllowedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot, ProjectionPolicy: resourcev1.ProjectionNone}},
		Limits:   resourcev1.WorkspaceLimits{MaxSessionsPerTenant: 1, MaxLeaseSeconds: 300, MaxSnapshotBytes: 1 << 20, MaxOverlayBytes: 1 << 20},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var signed, runtime any
	if json.Unmarshal(contributions[0].Descriptor, &signed) != nil || json.Unmarshal(service.Contribution().Descriptor, &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
		t.Fatalf("运行时 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contributions[0].Descriptor, service.Contribution().Descriptor)
	}
}
