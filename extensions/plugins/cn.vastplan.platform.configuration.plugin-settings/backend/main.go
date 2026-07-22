// Command pluginsettings starts the trusted generic plugin configuration coordinator.
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.configuration.plugin-settings/pluginsettings"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	service, err := pluginsettings.New("")
	if err != nil {
		log.Fatalf("初始化插件配置协调器失败: %v", err)
	}
	plugin := sdk.New(pluginsettings.PluginID, pluginsettings.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(pluginsettings.Contribution(service))
	if err := plugin.Serve(); err != nil {
		log.Fatalf("插件配置协调器退出: %v", err)
	}
}
