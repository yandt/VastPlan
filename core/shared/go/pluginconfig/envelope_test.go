package pluginconfig_test

import (
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

func TestParseIsolatesPluginConfigurationAndEnvironment(t *testing.T) {
	config := map[string]any{
		"plugins": map[string]any{
			"plugin.a": map[string]any{"retries": 3},
			"plugin.b": map[string]any{"region": "cn-east"},
		},
		"environment_allowlist": map[string]any{
			"plugin.b": []any{"B_TOKEN", "B_ENDPOINT"},
		},
		"managed_credentials": map[string]any{
			"plugin.a": map[string]any{"token": map[string]any{"handle": "credential://managed/opaque", "scope": "tenant", "owner": "plugin.a", "purpose": "example.token", "version": 2}},
		},
		"partition_keys": []any{"tenant-b", "tenant-a"},
	}
	envelope, err := pluginconfig.Parse(config, []string{"plugin.a", "plugin.b"})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Plugins["plugin.a"]["retries"] != float64(3) || len(envelope.Plugins["plugin.a"]) != 1 {
		t.Fatalf("plugin.a 配置投影错误: %#v", envelope.Plugins["plugin.a"])
	}
	if got := envelope.EnvironmentAllowlist["plugin.b"]; len(got) != 2 || got[0] != "B_ENDPOINT" || got[1] != "B_TOKEN" {
		t.Fatalf("plugin.b 环境授权错误: %#v", got)
	}
	if ref := envelope.ManagedCredentials["plugin.a"]["token"]; ref.Owner != "plugin.a" || ref.Purpose != "example.token" || ref.Version != 2 {
		t.Fatalf("plugin.a 托管凭证投影错误: %#v", ref)
	}
	if config["plugins"].(map[string]any)["plugin.a"].(map[string]any)["retries"] != 3 {
		t.Fatal("解析不得修改输入")
	}
}

func TestParseRejectsUnknownPluginAndLegacyFlatConfig(t *testing.T) {
	for _, config := range []map[string]any{
		{"plugins": map[string]any{"plugin.other": map[string]any{"token": "x"}}},
		{"environment_allowlist": []any{"TOKEN"}},
		{"managed_credentials": map[string]any{"plugin.a": map[string]any{"token": map[string]any{"handle": "credential://managed/x", "scope": "tenant", "owner": "plugin.other", "purpose": "x", "version": 1}}}},
		{"platform.settings.stateFile": "/tmp/settings.json"},
	} {
		if _, err := pluginconfig.Parse(config, []string{"plugin.a"}); err == nil {
			t.Fatalf("不安全配置必须被拒绝: %#v", config)
		}
	}
}
