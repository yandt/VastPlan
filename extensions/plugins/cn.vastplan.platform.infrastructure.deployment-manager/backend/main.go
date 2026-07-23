// Command deploymentmanager starts the platform node and bootstrap job service.
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.infrastructure.deployment-manager/deploymentmanager"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	service := deploymentmanager.New()
	plugin := sdk.New(deploymentmanager.PluginID, deploymentmanager.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(deploymentmanager.Contribution(service))
	if err := plugin.Serve(); err != nil {
		log.Fatalf("deployment-manager 退出: %v", err)
	}
}
