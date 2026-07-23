package nodeagent

import "testing"

func TestBackgroundServiceTenantIsBoundFromTrustedConfiguration(t *testing.T) {
	plugin := InstalledPlugin{ID: "com.example.controller", Contract: PluginRuntimeContract{BackgroundService: true}}
	tenantID, err := backgroundServiceTenant(plugin, map[string]any{"tenantId": "tenant-a"})
	if err != nil || tenantID != "tenant-a" {
		t.Fatalf("后台服务租户绑定失败: tenant=%q err=%v", tenantID, err)
	}
	for name, values := range map[string]map[string]any{
		"缺少":   {},
		"非字符串": {"tenantId": 1},
		"未规范":  {"tenantId": " tenant-a"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := backgroundServiceTenant(plugin, values); err == nil {
				t.Fatal("无可信 tenantId 时必须拒绝启动后台服务")
			}
		})
	}
}

func TestOrdinaryPluginDoesNotAcquireAutonomousTenant(t *testing.T) {
	plugin := InstalledPlugin{ID: "com.example.ordinary"}
	tenantID, err := backgroundServiceTenant(plugin, map[string]any{"tenantId": "tenant-a"})
	if err != nil || tenantID != "" {
		t.Fatalf("普通插件不得取得自主租户: tenant=%q err=%v", tenantID, err)
	}
}
