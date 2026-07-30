// Command versionledger starts the trusted Version Ledger foundation plugin.
package main

import (
	"log"

	ledger "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.ledger/versionledger"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var pluginVersion = ledger.PluginVersion

func main() {
	var configuration ledger.StartupConfiguration
	if err := sdk.DecodeStartupConfiguration(&configuration); err != nil {
		log.Fatalf("解析 Version Ledger 启动配置: %v", err)
	}
	service, err := ledger.BuildConfiguredService(configuration)
	if err != nil {
		log.Fatalf("初始化 Version Ledger: %v", err)
	}
	plugin := sdk.New(ledger.PluginID, pluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Version Ledger 退出: %v", err)
	}
}
