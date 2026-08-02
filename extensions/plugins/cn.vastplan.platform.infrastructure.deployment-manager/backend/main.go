// Command deploymentmanager starts the platform node and bootstrap job service.
package main

import (
	"log"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.infrastructure.deployment-manager/deploymentmanager"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var config struct {
		PluginInstallationApprovalPolicy *approvalv2.ProviderBinding `json:"pluginInstallationApprovalPolicy,omitempty"`
	}
	if err := sdk.DecodeStartupConfiguration(&config); err != nil {
		log.Fatalf("读取 deployment-manager 配置: %v", err)
	}
	if config.PluginInstallationApprovalPolicy != nil {
		if err := approvalv2.ValidateBinding(*config.PluginInstallationApprovalPolicy); err != nil {
			log.Fatalf("验证插件安装审批 Provider: %v", err)
		}
	}
	service := deploymentmanager.NewWithOptions(deploymentmanager.ServiceOptions{ApprovalBinding: config.PluginInstallationApprovalPolicy})
	plugin := sdk.New(deploymentmanager.PluginID, deploymentmanager.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(deploymentmanager.Contribution(service))
	if err := plugin.Serve(); err != nil {
		log.Fatalf("deployment-manager 退出: %v", err)
	}
}
