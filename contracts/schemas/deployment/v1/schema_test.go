package deploymentv1

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_NormalizesStableChannelAndMatchesNode(t *testing.T) {
	state, err := Parse([]byte(`{
  "version":1,
  "revision":42,
  "metadata":{"name":"local"},
  "units":[{
    "id":"backend-main","kind":"service","enabled":true,"service_role":"backend","replicas":1,
    "placement":{"nodeSelector":{"tier":"dev"}},
    "plugins":[{"id":"com.example.demo","version":"1.2.3"}]
  }]
}`))
	if err != nil {
		t.Fatalf("合法期望态应通过: %v", err)
	}
	unit := state.Units[0]
	if unit.Plugins[0].Channel != "stable" {
		t.Fatalf("空 channel 应规范化为 stable，实际 %q", unit.Plugins[0].Channel)
	}
	if !unit.MatchesNode(map[string]string{"tier": "dev"}) || unit.MatchesNode(map[string]string{"tier": "prod"}) {
		t.Fatal("nodeSelector 必须按标签全匹配")
	}
}

func TestParse_RejectsReplicasAboveOne(t *testing.T) {
	_, err := Parse([]byte(`{"version":1,"revision":1,"metadata":{"name":"local"},"units":[{"id":"x","kind":"service","enabled":true,"service_role":"backend","replicas":2,"plugins":[{"id":"com.example.demo","version":"1.0.0"}]}]}`))
	if err == nil {
		t.Fatal("本地 v1 不具备跨节点调度，replicas>1 必须 fail-closed")
	}
}

func TestParse_RejectsDuplicateUnitAndPluginIDs(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"unit 重复", `{"version":1,"revision":1,"metadata":{"name":"local"},"units":[{"id":"x","kind":"service","enabled":true,"service_role":"backend","replicas":1,"plugins":[{"id":"com.example.a","version":"1.0.0"}]},{"id":"x","kind":"service","enabled":true,"service_role":"backend","replicas":1,"plugins":[{"id":"com.example.b","version":"1.0.0"}]}]}`},
		{"插件重复", `{"version":1,"revision":1,"metadata":{"name":"local"},"units":[{"id":"x","kind":"service","enabled":true,"service_role":"backend","replicas":1,"plugins":[{"id":"com.example.a","version":"1.0.0"},{"id":"com.example.a","version":"1.1.0"}]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse([]byte(tc.raw)); err == nil {
				t.Fatal("重复逻辑标识必须被拒绝")
			}
		})
	}
}

func TestParse_ValidatesPluginScopedConfigurationEnvelope(t *testing.T) {
	valid := `{"version":1,"revision":1,"metadata":{"name":"local"},"units":[{"id":"x","kind":"service","enabled":true,"service_role":"backend","replicas":1,"config":{"plugins":{"com.example.a":{"retries":3}},"environment_allowlist":{"com.example.a":["EXAMPLE_TOKEN"]}},"plugins":[{"id":"com.example.a","version":"1.0.0"}]}]}`
	if _, err := Parse([]byte(valid)); err != nil {
		t.Fatalf("插件隔离配置信封应通过: %v", err)
	}
	invalid := `{"version":1,"revision":1,"metadata":{"name":"local"},"units":[{"id":"x","kind":"service","enabled":true,"service_role":"backend","replicas":1,"config":{"plugins":{"com.example.other":{"token":"secret"}}},"plugins":[{"id":"com.example.a","version":"1.0.0"}]}]}`
	if _, err := Parse([]byte(invalid)); err == nil {
		t.Fatal("未安装插件的配置必须被拒绝")
	}
}

func TestParseFileAcceptsNestedYAMLStartupConfiguration(t *testing.T) {
	root := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("desired.yaml", `
version: 1
revision: 5
metadata:
  $include: metadata.yaml
units:
  - $include: units/backend.yaml
`)
	write("metadata.yaml", "name: yaml-startup\n")
	write("units/backend.yaml", `
- id: backend-main
  kind: service
  enabled: true
  service_role: backend
  replicas: 1
  plugins:
    - id: com.example.demo
      version: 1.2.3
`)
	state, err := ParseFile(filepath.Join(root, "desired.yaml"))
	if err != nil {
		t.Fatalf("嵌套 YAML 启动配置应通过现有 Schema: %v", err)
	}
	if state.Metadata.Name != "yaml-startup" || len(state.Units) != 1 || state.Units[0].Plugins[0].Channel != "stable" {
		t.Fatalf("YAML 启动配置未走既有规范化: %+v", state)
	}
}

func TestUnitFingerprint_IgnoresPluginOrder(t *testing.T) {
	a := Unit{ID: "x", Kind: "service", Enabled: true, Replicas: 1, Plugins: []PluginRef{{ID: "com.example.a", Version: "1.0.0", Channel: "stable"}, {ID: "com.example.b", Version: "2.0.0", Channel: "stable"}}}
	b := a
	b.Plugins = []PluginRef{a.Plugins[1], a.Plugins[0]}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("插件声明顺序不应造成 service 无意义重启")
	}
}
