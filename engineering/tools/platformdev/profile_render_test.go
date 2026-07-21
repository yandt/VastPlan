package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configfile"
)

func TestRenderPlatformProfileProducesValidProviderComposition(t *testing.T) {
	template, err := os.ReadFile(filepath.Join("..", "..", "deploy", "platform-management-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := renderPlatformProfile(template, "/private/tmp/vastplan-dev", "/private/tmp/vastplan-state", "127.0.0.1:9443")
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
