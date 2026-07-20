package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
)

func TestRenderPlatformProfileProducesValidProviderComposition(t *testing.T) {
	template, err := os.ReadFile(filepath.Join("..", "..", "deploy", "platform-management-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw := renderPlatformProfile(template, "/private/tmp/vastplan-dev", "127.0.0.1:9443")
	if strings.Contains(string(raw), "__VASTPLAN_") {
		t.Fatal("渲染后的平台 Profile 不得保留占位符")
	}
	profile, err := backendcompositionv1.ParsePlatformProfile(raw)
	if err != nil {
		t.Fatalf("渲染后的平台 Profile 无效: %v", err)
	}
	for _, service := range profile.Services {
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
