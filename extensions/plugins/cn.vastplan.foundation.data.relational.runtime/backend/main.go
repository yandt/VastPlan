// Command databaseruntime starts the dedicated Database Runtime foundation
// plugin. The public surface still exposes Provider discovery only; query and
// transaction operations remain closed until the execution service is wired.
package main

import (
	"log"

	runtime "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	registry, err := runtime.NewDefaultRegistry(runtime.ProviderSecurityPolicy{})
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
