package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configfile"
)

func TestRenderPlatformProfileProducesValidProviderComposition(t *testing.T) {
	template, err := os.ReadFile(filepath.Join("..", "..", "deploy", "platform-management-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	portalCatalog, err := os.ReadFile(filepath.Join("..", "..", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := renderPlatformProfile(template, portalCatalog, "/private/tmp/vastplan-dev", "/private/tmp/vastplan-state", "127.0.0.1:9443")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "__VASTPLAN_") {
		t.Fatal("渲染后的平台 Profile 不得保留占位符")
	}
	profile, err := backendcompositionv1.ParsePlatformProfile(raw)
	if err != nil {
		t.Fatalf("渲染后的平台 Profile 无效: %v", err)
	}
	for _, service := range profile.Services {
		if service.ID == "platform-database-runtime" && service.Replicas != 1 {
			t.Fatalf("开发 Profile 必须把单节点 Database Runtime 缩放为 1: %#v", service)
		}
		if service.ID == "platform-api-exposure" {
			plugins := service.Config["plugins"].(map[string]any)
			exposure := plugins["cn.vastplan.platform.integration.api-exposure"].(map[string]any)
			if exposure["contractCatalogFile"] != "/private/tmp/vastplan-state/api-contract-catalog.json" {
				t.Fatalf("API Contract Catalog 必须使用跨重启稳定路径: %#v", exposure)
			}
		}
		if service.ID != "platform-artifacts" {
			continue
		}
		plugins := service.Config["plugins"].(map[string]any)
		repository := plugins["cn.vastplan.platform.artifacts.repository"].(map[string]any)
		if repository["listen"] != "127.0.0.1:9443" || repository["storageProvider"] != "platform.artifacts.storage.file" {
			t.Fatalf("制品仓库插件配置未正确渲染: %#v", repository)
		}
		return
	}
	t.Fatal("平台 Profile 缺少 platform-artifacts service")
}

func TestPlatformManagementDeploymentCountsEnabledServices(t *testing.T) {
	runDir := t.TempDir()
	raw := []byte(`{
  "version": 1,
  "revision": 12,
  "id": "test-profile",
  "target": {"kernel": "backend"},
  "serviceClasses": ["application.backend"],
  "attachments": [],
  "services": [
    {"id":"enabled","kind":"service","enabled":true,"service_role":"backend","logical_service":"test.enabled","instance_policy":"per-kernel","state_model":"local-ephemeral","visibility":"local","routing":"direct","replicas":1,"plugins":[{"id":"cn.vastplan.test.enabled","version":"1.0.0","channel":"stable"}]},
    {"id":"disabled","kind":"service","enabled":false,"service_role":"backend","logical_service":"test.disabled","instance_policy":"per-kernel","state_model":"local-ephemeral","visibility":"local","routing":"direct","replicas":1,"plugins":[{"id":"cn.vastplan.test.disabled","version":"1.0.0","channel":"stable"}]}
  ]
}`)
	if err := os.WriteFile(filepath.Join(runDir, "platform-management-profile.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := runtime{runDir: runDir}
	revision, units, err := runtime.platformManagementDeployment()
	if err != nil {
		t.Fatal(err)
	}
	if revision != "12" || units != 1 {
		t.Fatalf("部署摘要错误: revision=%s units=%d", revision, units)
	}
}

func TestPortalPlatformBindingUsesCurrentProfileDigest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var catalog frontendcompositionv1.PortalPlatformCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Profiles) != 1 || len(catalog.Bindings) != 1 {
		t.Fatalf("开发 Portal Catalog 必须包含唯一 Profile 与 Binding: profiles=%d bindings=%d", len(catalog.Profiles), len(catalog.Bindings))
	}
	profile, binding := catalog.Profiles[0], catalog.Bindings[0]
	digest := profile.Digest()
	if binding.PlatformProfile.ID != profile.ID || binding.PlatformProfile.Revision != profile.Revision || binding.PlatformProfile.Digest != digest {
		t.Fatalf("Portal Binding 未引用当前 Profile: want=%s@%d/%s got=%s@%d/%s", profile.ID, profile.Revision, digest, binding.PlatformProfile.ID, binding.PlatformProfile.Revision, binding.PlatformProfile.Digest)
	}
}

func TestWriteSeedRepositoryProfileUsesPrivateRunPaths(t *testing.T) {
	runDir := t.TempDir()
	runtime := runtime{runDir: runDir, options: options{seedArtifactListen: "127.0.0.1:18442"}}
	if err := runtime.writeSeedRepositoryProfile(); err != nil {
		t.Fatal(err)
	}
	raw, err := configfile.Load(filepath.Join(runDir, "seed-repository.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatal(err)
	}
	if profile["listen"] != "127.0.0.1:18442" || profile["repositoryRoot"] != filepath.Join(runDir, "repository") {
		t.Fatalf("Seed Profile 路径或监听地址错误: %s", raw)
	}
	if profile["trustFile"] != filepath.Join(runDir, "secrets", "seed-artifact-trust.json") {
		t.Fatalf("Seed Profile 只能加载 Seed-only 信任文档: %s", raw)
	}
	info, err := os.Stat(filepath.Join(runDir, "seed-repository.yaml"))
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("Seed Profile 必须仅属主可访问: info=%v err=%v", info, err)
	}
}
