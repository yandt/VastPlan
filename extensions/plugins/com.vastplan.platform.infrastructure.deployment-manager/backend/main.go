// Command deploymentmanager starts the platform node and bootstrap job service.
package main

import (
	"log"
	"os"

	"cdsoft.com.cn/VastPlan/extensions/plugins/com.vastplan.platform.infrastructure.deployment-manager/deploymentmanager"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	service, err := deploymentmanager.New(os.Getenv("VASTPLAN_DEPLOYMENT_MANAGER_STATE_FILE"))
	if err != nil {
		log.Fatalf("初始化 deployment-manager 失败: %v", err)
	}
	plugin := sdk.New(deploymentmanager.PluginID, deploymentmanager.PluginVersion, map[string]string{"backend": "^1.0"})
	plugin.Contribute(deploymentmanager.Contribution(service))
	if err := plugin.Serve(); err != nil {
		log.Fatalf("deployment-manager 退出: %v", err)
	}
}
