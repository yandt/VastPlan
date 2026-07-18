package deploymentv1

import "testing"

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

func TestUnitFingerprint_IgnoresPluginOrder(t *testing.T) {
	a := Unit{ID: "x", Kind: "service", Enabled: true, Replicas: 1, Plugins: []PluginRef{{ID: "com.example.a", Version: "1.0.0", Channel: "stable"}, {ID: "com.example.b", Version: "2.0.0", Channel: "stable"}}}
	b := a
	b.Plugins = []PluginRef{a.Plugins[1], a.Plugins[0]}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("插件声明顺序不应造成 service 无意义重启")
	}
}
