// Package main starts the database authentication Provider runtime.
package main

import (
	"log"

	databaseprovider "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authentication.provider.database/databaseprovider"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var configuration databaseprovider.Configuration
	if err := sdk.DecodeStartupConfiguration(&configuration); err != nil {
		log.Fatalf("读取 Database Authentication 配置: %v", err)
	}
	provider, err := databaseprovider.New(configuration)
	if err != nil {
		log.Fatalf("初始化 Database Authentication Provider: %v", err)
	}
	plugin := sdk.New(databaseprovider.PluginID, databaseprovider.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(provider.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Database Authentication Provider 退出: %v", err)
	}
}
