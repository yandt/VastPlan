// Command compositionplanner 启动无状态的应用组合规划插件进程。
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.infrastructure.composition-planner/planner"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var config planner.Config
	if err := sdk.DecodeStartupConfiguration(&config); err != nil {
		log.Fatalf("读取 Composition Planner 配置: %v", err)
	}
	service, err := planner.New(config)
	if err != nil {
		log.Fatalf("初始化 Composition Planner: %v", err)
	}
	plugin := sdk.New(planner.PluginID, planner.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(planner.Contribution(service))
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Composition Planner 退出: %v", err)
	}
}
