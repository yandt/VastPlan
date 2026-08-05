// Command interaction-broker starts the platform interaction coordination plugin.
package main

import (
	"log"

	interaction "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.interaction.broker/interaction"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	service := interaction.New()
	p := sdk.New(interaction.PluginID, interaction.PluginVersion, map[string]string{"backend": "^0.1"})
	p.Contribute(interaction.Contribution(service))
	if err := p.Serve(); err != nil {
		log.Fatalf("Interaction Broker 插件退出: %v", err)
	}
}
