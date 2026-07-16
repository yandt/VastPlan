package pluginv1

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseManifest_ExistingPluginsConform(t *testing.T) {
	pluginsDir := filepath.Join("..", "..", "..", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatalf("读取示例插件目录失败: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(pluginsDir, entry.Name(), "vastplan.plugin.json"))
		if err != nil {
			t.Fatalf("读取 %s 清单失败: %v", entry.Name(), err)
		}
		manifest, err := ParseManifest(raw)
		if err != nil {
			t.Errorf("%s 清单应符合 Schema: %v", entry.Name(), err)
			continue
		}
		if manifest.ID != entry.Name() {
			t.Errorf("清单 ID=%q，应与目录名 %q 一致", manifest.ID, entry.Name())
		}
	}
}

func TestParseManifest_RejectsUnknownField(t *testing.T) {
	raw := []byte(`{"id":"com.example.demo","name":"demo","description":"demo","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}},"unexpected":true}`)
	if _, err := ParseManifest(raw); err == nil {
		t.Fatal("未知字段必须被 Schema 拒绝")
	}
}

func TestValidateDescriptor_RejectsInvalidHookPhase(t *testing.T) {
	err := ValidateDescriptor("hook", []byte(`{"point":"invoke","phase":"later"}`))
	if err == nil {
		t.Fatal("非法 hook phase 必须被 Schema 拒绝")
	}
}

func TestValidateDescriptor_AllowsFuturePointWithJSONObject(t *testing.T) {
	if err := ValidateDescriptor("future.point", []byte(`{"vendorField":"kept"}`)); err != nil {
		t.Fatalf("未实现的未来扩展点应只要求 descriptor 为对象，实际: %v", err)
	}
}
