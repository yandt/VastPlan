// Command versionworkspace starts the trusted Version Workspace foundation plugin.
package main

import (
	"log"

	workspace "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.workspace/versionworkspace"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var pluginVersion = workspace.PluginVersion

func main() {
	var configuration workspace.StartupConfiguration
	if err := sdk.DecodeStartupConfiguration(&configuration); err != nil {
		log.Fatalf("解析 Version Workspace 启动配置: %v", err)
	}
	service, err := workspace.BuildConfiguredService(configuration)
	if err != nil {
		log.Fatalf("初始化 Version Workspace: %v", err)
	}
	plugin := sdk.New(workspace.PluginID, pluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Version Workspace 退出: %v", err)
	}
}
