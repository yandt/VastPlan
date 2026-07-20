package plugin

import (
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
)

func TestShutdownBeforeServePreventsSharedHostUnitStart(t *testing.T) {
	plugin := New("cn.vastplan.test.shared-unit", "1.0.0", map[string]string{"backend": "^1.0"})
	plugin.Shutdown()
	err := plugin.ServeWithEnvironment(map[string]string{
		protocol.MagicEnvKey:    protocol.MagicCookie,
		protocol.HostAddrEnvKey: "127.0.0.1:1",
	})
	if err == nil || !strings.Contains(err.Error(), "已请求停止") {
		t.Fatalf("停止请求必须阻止逻辑单元建立会话: %v", err)
	}
}

func TestDecodeStartupConfigurationFailsClosedOnUnknownFields(t *testing.T) {
	var config struct {
		Listen string `json:"listen"`
	}
	if err := decodeStartupConfiguration(`{"listen":"127.0.0.1:8443"}`, &config); err != nil || config.Listen == "" {
		t.Fatalf("合法启动配置解析失败: config=%+v err=%v", config, err)
	}
	if err := decodeStartupConfiguration(`{"listen":"127.0.0.1:8443","secret":"leak"}`, &config); err == nil {
		t.Fatal("未知启动配置字段必须 fail-closed")
	}
}
