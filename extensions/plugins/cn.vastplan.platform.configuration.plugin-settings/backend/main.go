// Command pluginsettings starts the trusted generic plugin configuration coordinator.
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.configuration.plugin-settings/pluginsettings"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	service := pluginsettings.New()
	plugin := sdk.New(pluginsettings.PluginID, pluginsettings.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(pluginsettings.Contribution(service))
	plugin.Contribute(pluginsettings.ScopedContribution(service))
	if err := plugin.Serve(); err != nil {
		log.Fatalf("插件配置协调器退出: %v", err)
	}
}
