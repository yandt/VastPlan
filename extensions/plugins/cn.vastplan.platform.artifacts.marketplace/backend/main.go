// Command marketplace starts the trusted multi-source plugin marketplace.
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.marketplace/marketplace"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var config marketplace.Config
	if err := sdk.DecodeStartupConfiguration(&config); err != nil {
		log.Fatalf("读取 Marketplace 配置: %v", err)
	}
	service, err := marketplace.New(config)
	if err != nil {
		log.Fatalf("初始化 Marketplace: %v", err)
	}
	plugin := sdk.New(marketplace.PluginID, marketplace.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Marketplace 退出: %v", err)
	}
}
