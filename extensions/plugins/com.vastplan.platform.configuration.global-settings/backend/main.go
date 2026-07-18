// Command globalsettings 启动租户隔离的全局设置基础插件进程。
package main

import (
	"log"

	settings "cdsoft.com.cn/VastPlan/extensions/plugins/com.vastplan.platform.configuration.global-settings/settings"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	// 状态位置在首个调用时从受认证的 kernel.config.get 读取；不接受环境变量。
	service, err := settings.New("")
	if err != nil {
		log.Fatalf("初始化全局设置服务失败: %v", err)
	}
	p := sdk.New(settings.PluginID, settings.PluginVersion, map[string]string{"backend": "^0.1"})
	p.Contribute(settings.Contribution(service))
	if err := p.Serve(); err != nil {
		log.Fatalf("全局设置插件退出: %v", err)
	}
}
