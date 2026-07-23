package compositionresolver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type artifactReader map[string]pluginv1.Artifact

func (r artifactReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	artifact, ok := r[ref.PluginID+"@"+ref.Version+"/"+ref.Channel]
	if !ok {
		return pluginv1.Artifact{}, nil, errors.New("not found")
	}
	return artifact, nil, nil
}

func manifest(id, publisher string) []byte {
	return []byte(fmt.Sprintf(`{
		"id":%q,"name":"plugin","description":"plugin","version":"1.0.0","publisher":%q,
		"engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"tools":[{"id":"tool.%s","service_role":"backend","title":"tool","subcommands":[]}]}}
	}`, id, publisher, strings.ReplaceAll(id, ".", "-")))
}

func ref(id string) deploymentv1.PluginRef {
	return deploymentv1.PluginRef{ID: id, Version: "1.0.0", Channel: "stable"}
}

func artifact(id, publisher string) pluginv1.Artifact {
	return pluginv1.Artifact{PluginID: id, Version: "1.0.0", Channel: "stable", Manifest: manifest(id, publisher)}
}

func baseInputs() (backendcompositionv1.PlatformProfile, backendcompositionv1.ApplicationComposition, artifactReader) {
	platformID := "cn.vastplan.foundation.security.access-policy"
	applicationID := "com.example.agent"
	profile := backendcompositionv1.PlatformProfile{
		Document: compositioncommonv1.Document{Version: 1, Revision: 2, ID: "backend-default"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend},
		ServiceClasses: []string{"application.backend"},
		Attachments:    []backendcompositionv1.Attachment{{ServiceClass: "application.backend", Plugins: []deploymentv1.PluginRef{ref(platformID)}}},
		Services:       []deploymentv2.ServiceUnit{},
	}
	application := backendcompositionv1.ApplicationComposition{
		Document: compositioncommonv1.Document{Version: 1, Revision: 4, ID: "agent-studio"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend}, Metadata: deploymentv1.Metadata{Name: "agent-studio", Tenant: "acme"},
		Units: []backendcompositionv1.ApplicationUnit{{ServiceClass: "application.backend", Spec: deploymentv2.ServiceUnit{ID: "api", Kind: "service", Plugins: []deploymentv1.PluginRef{ref(applicationID)}, Enabled: true, ServiceRole: "backend", Replicas: 1}}},
	}
	reader := artifactReader{
		platformID + "@1.0.0/stable":    artifact(platformID, "vastplan"),
		applicationID + "@1.0.0/stable": artifact(applicationID, "example"),
	}
	return profile, application, reader
}

func TestResolveInjectsPlatformAttachmentsAndLocksOrigins(t *testing.T) {
	profile, application, reader := baseInputs()
	resolved, err := Resolve(profile, application, 9, reader, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Revision != 9 || len(resolved.Units) != 1 || len(resolved.Units[0].Plugins) != 2 {
		t.Fatalf("解析结果不完整: %+v", resolved)
	}
	if resolved.Resolution.PluginOrigins[resolved.Units[0].Plugins[0].ID] != deploymentv2.OriginPlatformProfile || resolved.Resolution.PluginOrigins[resolved.Units[0].Plugins[1].ID] != deploymentv2.OriginApplication {
		t.Fatalf("插件来源锁错误: %+v", resolved.Resolution.PluginOrigins)
	}
	if len(resolved.Resolution.PlatformProfile.Digest) != 64 || len(resolved.Resolution.ApplicationComposition.Digest) != 64 {
		t.Fatalf("输入摘要未锁定: %+v", resolved.Resolution)
	}
}

func TestResolveRejectsApplicationPlatformOverrideAndDevelopmentByDefault(t *testing.T) {
	profile, application, reader := baseInputs()
	platformID := profile.Attachments[0].Plugins[0].ID
	application.Units[0].Spec.Plugins = []deploymentv1.PluginRef{ref(platformID)}
	if _, err := Resolve(profile, application, 1, reader, Options{}); err == nil || !strings.Contains(err.Error(), "不能覆盖平台插件") {
		t.Fatalf("应用覆盖平台插件必须拒绝: %v", err)
	}

	profile, application, reader = baseInputs()
	developmentID := "cn.vastplan.example.demo.hello"
	application.Units[0].Spec.Plugins = []deploymentv1.PluginRef{ref(developmentID)}
	reader[developmentID+"@1.0.0/stable"] = artifact(developmentID, "vastplan")
	if _, err := Resolve(profile, application, 1, reader, Options{}); err == nil || !strings.Contains(err.Error(), "开发插件") {
		t.Fatalf("生产默认必须拒绝 example: %v", err)
	}
	if _, err := Resolve(profile, application, 1, reader, Options{AllowDevelopmentPlugins: true}); err != nil {
		t.Fatalf("显式开发模式应允许 example: %v", err)
	}
}

func TestResolveRejectsServiceClassAndUnitCollisions(t *testing.T) {
	profile, application, reader := baseInputs()
	application.Units[0].ServiceClass = "unknown"
	if _, err := Resolve(profile, application, 1, reader, Options{}); err == nil {
		t.Fatal("未授权 serviceClass 必须拒绝")
	}

	profile, application, reader = baseInputs()
	platformServiceID := "cn.vastplan.platform.settings.service"
	profile.Services = []deploymentv2.ServiceUnit{{ID: "api", Kind: "service", Plugins: []deploymentv1.PluginRef{ref(platformServiceID)}, Enabled: true, ServiceRole: "backend", Replicas: 1}}
	reader[platformServiceID+"@1.0.0/stable"] = artifact(platformServiceID, "vastplan")
	if _, err := Resolve(profile, application, 1, reader, Options{}); err == nil || !strings.Contains(err.Error(), "与应用 unit 冲突") {
		t.Fatalf("平台和应用 unit 冲突必须拒绝: %v", err)
	}
}

func TestResolveRejectsPlatformPluginWithConflictingTopology(t *testing.T) {
	profile, application, reader := baseInputs()
	platformID := profile.Attachments[0].Plugins[0].ID
	profile.Services = []deploymentv2.ServiceUnit{{ID: "policy-service", Kind: "service", Plugins: []deploymentv1.PluginRef{ref(platformID)}, Enabled: true, ServiceRole: "backend", Replicas: 1}}
	if _, err := Resolve(profile, application, 1, reader, Options{}); err == nil || !strings.Contains(err.Error(), "同时作为本地 attachment 和独立 service") {
		t.Fatalf("同一平台插件的本地与共享拓扑冲突必须拒绝: %v", err)
	}
}

func TestResolveAllowsExactLocalPermissionPluginInMultiplePlatformServices(t *testing.T) {
	profile, application, reader := baseInputs()
	policyID := "cn.vastplan.foundation.security.platform-admin-access-policy"
	policyManifest := []byte(`{
		"id":"cn.vastplan.foundation.security.platform-admin-access-policy","name":"policy","description":"policy",
		"version":"1.0.0","publisher":"vastplan","engines":{"backend":"^1.0"},
		"runtime":{"instancePolicy":"per-kernel","stateModel":"local-ephemeral","visibility":"local","routing":"direct"},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},
		"contributes":{"backend":{"permissionCheckers":[{"id":"platform.admin","service_role":"backend","title":"policy","priority":1000,"applies":{}}]}}
	}`)
	reader[policyID+"@1.0.0/stable"] = pluginv1.Artifact{PluginID: policyID, Version: "1.0.0", Channel: "stable", Manifest: policyManifest}
	profile.Services = []deploymentv2.ServiceUnit{
		{ID: "settings", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1, Plugins: []deploymentv1.PluginRef{ref(policyID)}},
		{ID: "credentials", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1, Plugins: []deploymentv1.PluginRef{ref(policyID)}},
	}
	resolved, err := Resolve(profile, application, 1, reader, Options{})
	if err != nil {
		t.Fatalf("同一精确本地权限插件应可保护多个平台 unit: %v", err)
	}
	if len(resolved.Units) != 3 {
		t.Fatalf("平台 unit 未完整保留: %+v", resolved.Units)
	}
}

func TestDeploySampleIsResolverOutput(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	profile, err := backendcompositionv1.ParsePlatformProfileFile(filepath.Join(root, "engineering", "deploy", "platform-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	application, err := backendcompositionv1.ParseApplicationCompositionFile(filepath.Join(root, "engineering", "deploy", "application-composition.json"))
	if err != nil {
		t.Fatal(err)
	}
	reader := artifactReader{}
	for _, item := range []struct{ id, version string }{{"cn.vastplan.demo-permission", "0.1.0"}, {"cn.vastplan.hello-world", "0.2.0"}} {
		id := item.id
		raw, readErr := os.ReadFile(filepath.Join(root, "extensions", "plugins", id, "vastplan.plugin.json"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		reader[id+"@"+item.version+"/stable"] = pluginv1.Artifact{PluginID: id, Version: item.version, Channel: "stable", Manifest: raw}
	}
	resolved, err := Resolve(profile, application, 1, reader, Options{AllowDevelopmentPlugins: true})
	if err != nil {
		t.Fatal(err)
	}
	want, err := deploymentv2.ParseFile(filepath.Join(root, "engineering", "deploy", "cluster.deployment.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved, want) {
		t.Fatalf("engineering/deploy/cluster.deployment.json 必须由当前双输入精确生成\nresolved=%+v\nwant=%+v", resolved, want)
	}
}
