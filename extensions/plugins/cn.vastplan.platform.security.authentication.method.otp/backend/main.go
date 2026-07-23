// Package main starts the enterprise one-time-code authentication Provider.
package main

import (
	"log"

	otpprovider "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.authentication.method.otp/otpprovider"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var configuration otpprovider.Configuration
	if err := sdk.DecodeStartupConfiguration(&configuration); err != nil {
		log.Fatalf("读取 OTP Provider 配置: %v", err)
	}
	provider, err := otpprovider.New(configuration)
	if err != nil {
		log.Fatalf("初始化 OTP Provider: %v", err)
	}
	plugin := sdk.New(otpprovider.PluginID, otpprovider.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(provider.Contribution())
	configurationContribution, err := provider.ConfigurationContribution()
	if err != nil {
		log.Fatalf("初始化 OTP configuration.v1 控制器: %v", err)
	}
	plugin.Contribute(configurationContribution)
	if err := plugin.Serve(); err != nil {
		log.Fatalf("OTP Provider 退出: %v", err)
	}
}
