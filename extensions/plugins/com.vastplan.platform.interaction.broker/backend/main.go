// Command interaction-broker starts the platform interaction coordination plugin.
package main

import (
	"log"

	interaction "cdsoft.com.cn/VastPlan/extensions/plugins/com.vastplan.platform.interaction.broker/interaction"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	service, err := interaction.New("")
	if err != nil {
		log.Fatalf("初始化 Interaction Broker 服务失败: %v", err)
	}
	p := sdk.New(interaction.PluginID, interaction.PluginVersion, map[string]string{"backend": "^1.0"})
	p.Contribute(interaction.Contribution(service))
	if err := p.Serve(); err != nil {
		log.Fatalf("Interaction Broker 插件退出: %v", err)
	}
}
