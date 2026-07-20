// Command databaseruntime starts the dedicated Database Runtime foundation
// plugin. A3 exposes management activation and stateless execution; signed
// instance-affine transactions remain closed for the next phase.
package main

import (
	"log"

	runtime "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var startup struct {
		AllowInsecureTLS bool `json:"allowInsecureTLS"`
	}
	if err := sdk.DecodeStartupConfiguration(&startup); err != nil {
		log.Fatalf("解析 Database Runtime 启动配置: %v", err)
	}
	registry, err := runtime.NewDefaultRegistry(runtime.ProviderSecurityPolicy{AllowInsecureTLS: startup.AllowInsecureTLS})
	if err != nil {
		log.Fatalf("注册 Database Provider: %v", err)
	}
	service, err := runtime.NewService(registry)
	if err != nil {
		log.Fatalf("初始化 Database Runtime: %v", err)
	}
	plugin := sdk.New(runtime.PluginID, runtime.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Database Runtime 退出: %v", err)
	}
}
