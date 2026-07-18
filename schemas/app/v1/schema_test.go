package appv1

import "testing"

func TestParseRunnerProfile(t *testing.T) {
	profile, err := Parse([]byte(`{"version":1,"revision":3,"id":"collector","tenantId":"tenant-a","runtime":"runner","targets":["darwin/arm64","linux/amd64"],"distribution":"self-update","assignedTo":["runner-a"],"plugins":[{"id":"com.vastplan.collector","version":"1.2.0"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if profile.Plugins[0].Channel != "stable" || len(profile.Digest()) != 64 {
		t.Fatalf("Profile 未规范化: %+v digest=%q", profile, profile.Digest())
	}
}

func TestParseRejectsInvalidRunnerProfile(t *testing.T) {
	for name, raw := range map[string]string{
		"unknown field":    `{"version":1,"revision":1,"id":"collector","tenantId":"tenant-a","runtime":"runner","targets":["linux/amd64"],"distribution":"self-update","assignedTo":["runner-a"],"plugins":[{"id":"com.vastplan.collector","version":"1.0.0"}],"unexpected":true}`,
		"mobile runtime":   `{"version":1,"revision":1,"id":"collector","tenantId":"tenant-a","runtime":"mobile","targets":["linux/amd64"],"distribution":"self-update","assignedTo":["runner-a"],"plugins":[{"id":"com.vastplan.collector","version":"1.0.0"}]}`,
		"invalid target":   `{"version":1,"revision":1,"id":"collector","tenantId":"tenant-a","runtime":"runner","targets":["freebsd/amd64"],"distribution":"self-update","assignedTo":["runner-a"],"plugins":[{"id":"com.vastplan.collector","version":"1.0.0"}]}`,
		"duplicate plugin": `{"version":1,"revision":1,"id":"collector","tenantId":"tenant-a","runtime":"runner","targets":["linux/amd64"],"distribution":"self-update","assignedTo":["runner-a"],"plugins":[{"id":"com.vastplan.collector","version":"1.0.0"},{"id":"com.vastplan.collector","version":"2.0.0"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw)); err == nil {
				t.Fatal("非法 Runner Profile 必须 fail-closed")
			}
		})
	}
}

func TestValidateUsesTheSameContractForGoValues(t *testing.T) {
	profile := Profile{Version: 1, Revision: 1, ID: "collector", TenantID: "tenant-a", Runtime: "runner", Targets: []string{"freebsd/amd64"}, Distribution: "self-update", AssignedTo: []string{"runner-a"}, Plugins: []PluginRef{{ID: "com.vastplan.collector", Version: "1.0.0"}}}
	if _, err := Validate(profile); err == nil {
		t.Fatal("直接构造的 Go Profile 也必须经过同一 Schema")
	}
}
