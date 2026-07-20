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
