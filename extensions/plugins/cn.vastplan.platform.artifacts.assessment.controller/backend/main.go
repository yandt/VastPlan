// Command artifactassessmentcontroller starts the leader-owned rescan loop.
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.assessment.controller/controller"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var config controller.Config
	if err := sdk.DecodeStartupConfiguration(&config); err != nil {
		log.Fatalf("读取 Assessment Controller 配置: %v", err)
	}
	service, err := controller.New(config)
	if err != nil {
		log.Fatalf("初始化 Assessment Controller: %v", err)
	}
	plugin := sdk.New(controller.PluginID, controller.PluginVersion, map[string]string{"backend": "^0.1"})
	if err := service.Bind(plugin); err != nil {
		log.Fatalf("绑定 Assessment Controller ports: %v", err)
	}
	plugin.Contribute(service.Contribution())
	plugin.OnLifecycle(service.Lifecycle())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Assessment Controller 退出: %v", err)
	}
}
