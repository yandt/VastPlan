package configfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, root, name, contents string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadYAMLSplicesNestedIncludesIntoCanonicalJSON(t *testing.T) {
	root := t.TempDir()
	main := writeConfig(t, root, "desired.yaml", `
version: 1
revision: 7
metadata:
  $include: fragments/metadata.yaml
units:
  - $include: units/platform.yaml
`)
	writeConfig(t, root, "fragments/metadata.yaml", "name: yaml-local\n")
	writeConfig(t, root, "units/platform.yaml", `
- id: platform-settings
  enabled: true
  config:
    $include: ../configs/settings.yaml
`)
	writeConfig(t, root, "configs/settings.yaml", "plugins:\n  cn.vastplan.example:\n    enabled: true\n")
	raw, err := Load(main)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["metadata"].(map[string]any)["name"] != "yaml-local" || len(got["units"].([]any)) != 1 {
		t.Fatalf("嵌套 include 未规范化: %s", raw)
	}
	if got["units"].([]any)[0].(map[string]any)["config"].(map[string]any)["plugins"] == nil {
		t.Fatalf("include 内部的 include 未展开: %s", raw)
	}
}

func TestLoadRejectsUnsafeYAMLFeaturesAndEscapingIncludes(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"alias.yaml":     "a: &shared value\nb: *shared\n",
		"duplicate.yaml": "a: 1\na: 2\n",
		"outside.yaml":   "$include: ../outside.yaml\n",
	} {
		path := writeConfig(t, root, name, contents)
		if _, err := Load(path); err == nil {
			t.Fatalf("%s 必须拒绝", name)
		}
	}
	path := writeConfig(t, root, "timestamp.yaml", "created: 2026-07-20\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "引号") {
		t.Fatalf("隐式 timestamp 必须要求显式字符串: %v", err)
	}
}

func TestLoadKeepsJSONCompatible(t *testing.T) {
	path := writeConfig(t, t.TempDir(), "desired.json", `{"revision":9007199254740993,"ok":true}`)
	raw, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"ok":true,"revision":9007199254740993}` {
		t.Fatalf("JSON 大整数或规范化失败: %s", raw)
	}
}
