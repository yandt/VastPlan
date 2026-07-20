// Command databaseruntime starts the dedicated Database Runtime foundation
// plugin. A3 exposes management activation and stateless execution; signed
// instance-affine transactions use the host-issued Runtime audience.
package main

import (
	"log"
	"os"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
	runtime "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var startup struct {
		AllowInsecureTLS bool `json:"allowInsecureTLS"`
		MaxTransactions  int  `json:"maxTransactions"`
	}
	if err := sdk.DecodeStartupConfiguration(&startup); err != nil {
		log.Fatalf("解析 Database Runtime 启动配置: %v", err)
	}
	registry, err := runtime.NewDefaultRegistry(runtime.ProviderSecurityPolicy{AllowInsecureTLS: startup.AllowInsecureTLS})
	if err != nil {
		log.Fatalf("注册 Database Provider: %v", err)
	}
	service, err := runtime.NewService(registry, runtime.ServiceOptions{
		InstanceID: os.Getenv(protocol.RuntimeAudienceEnvKey), MaxTransactions: startup.MaxTransactions,
	})
	if err != nil {
		log.Fatalf("初始化 Database Runtime: %v", err)
	}
	defer service.Close()
	plugin := sdk.New(runtime.PluginID, runtime.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Database Runtime 退出: %v", err)
	}
}
