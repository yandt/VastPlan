// Command databaseruntime starts the dedicated Database Runtime foundation
// plugin. Phase 1 exposes contract/provider discovery only and cannot decrypt
// credentials or open physical database connections.
package main

import (
	"log"

	runtime "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	service, err := runtime.NewService(runtime.NewRegistry())
	if err != nil {
		log.Fatalf("初始化 Database Runtime: %v", err)
	}
	plugin := sdk.New(runtime.PluginID, runtime.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Database Runtime 退出: %v", err)
	}
}
