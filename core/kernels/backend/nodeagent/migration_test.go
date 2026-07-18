package nodeagent

import (
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestPluginStateIdentityConversionPreservesSchemaIdentity(t *testing.T) {
	contract := pluginv1.StateIdentity{Format: "com.example.state", FormatVersion: 2}
	actualState := pluginStateIdentity(contract)
	if got := actualState.contractIdentity(); got != contract {
		t.Fatalf("状态身份转换漂移: got=%+v want=%+v", got, contract)
	}
}

func statefulPlugin(id, version, format string, formatVersion int32, from ...PluginStateIdentity) InstalledPlugin {
	return InstalledPlugin{
		ID: id, Version: version,
		State: &PluginStateContract{
			PluginStateIdentity: PluginStateIdentity{Format: format, FormatVersion: formatVersion},
			MigrationProtocol:   stateMigrationProtocolV1,
			MigrationFrom:       from,
		},
	}
}

func TestPlanStateMigrations(t *testing.T) {
	v1 := statefulPlugin("com.example.demo", "1.0.0", "com.example.demo.state", 1)
	v2 := statefulPlugin("com.example.demo", "2.0.0", "com.example.demo.state", 2,
		PluginStateIdentity{Format: "com.example.demo.state", FormatVersion: 1})
	plans, err := planStateMigrations("backend-main", "next", []InstalledPlugin{v1}, []InstalledPlugin{v2})
	if err != nil || len(plans) != 1 {
		t.Fatalf("合法状态升级计划 = %+v, %v", plans, err)
	}
	plan := plans[0]
	if plan.PluginID != v2.ID || plan.From.FormatVersion != 1 || plan.To.FormatVersion != 2 || plan.TransactionID == "" {
		t.Fatalf("迁移计划字段不完整: %+v", plan)
	}
	again, err := planStateMigrations("backend-main", "next", []InstalledPlugin{v1}, []InstalledPlugin{v2})
	if err != nil || again[0].TransactionID != plan.TransactionID {
		t.Fatalf("相同逻辑升级必须得到稳定事务 ID: first=%+v again=%+v err=%v", plan, again, err)
	}
}

func TestPlanStateMigrationsFailClosed(t *testing.T) {
	v1 := statefulPlugin("com.example.demo", "1.0.0", "com.example.demo.state", 1)
	tests := map[string]InstalledPlugin{
		"状态声明消失": {ID: v1.ID, Version: "2.0.0"},
		"未声明旧格式": statefulPlugin(v1.ID, "2.0.0", "com.example.demo.state", 2),
		"协议未知": func() InstalledPlugin {
			plugin := statefulPlugin(v1.ID, "2.0.0", "com.example.demo.state", 2,
				PluginStateIdentity{Format: "com.example.demo.state", FormatVersion: 1})
			plugin.State.MigrationProtocol = "future.v2"
			return plugin
		}(),
	}
	for name, candidate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := planStateMigrations("backend-main", "next", []InstalledPlugin{v1}, []InstalledPlugin{candidate}); err == nil {
				t.Fatal("不完整迁移契约必须 fail-closed")
			}
		})
	}
}

func TestPlanStateMigrationsSkipsInitialAndUnchangedState(t *testing.T) {
	state := statefulPlugin("com.example.demo", "1.1.0", "com.example.demo.state", 1)
	if plans, err := planStateMigrations("unit", "a", nil, []InstalledPlugin{state}); err != nil || len(plans) != 0 {
		t.Fatalf("首次引入状态无需迁移: plans=%v err=%v", plans, err)
	}
	old := state
	old.Version = "1.0.0"
	if plans, err := planStateMigrations("unit", "b", []InstalledPlugin{old}, []InstalledPlugin{state}); err != nil || len(plans) != 0 {
		t.Fatalf("格式不变无需迁移: plans=%v err=%v", plans, err)
	}
}
