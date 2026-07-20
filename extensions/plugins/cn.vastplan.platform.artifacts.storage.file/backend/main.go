// Command artifactstoragefile starts the local-file artifact storage provider.
package main

import (
	"log"
	"os"

	provider "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.storage.file/provider"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var startup struct {
		VolumeID string `json:"volumeId"`
	}
	if err := sdk.DecodeStartupConfiguration(&startup); err != nil {
		log.Fatalf("读取本地文件制品存储 Provider 配置: %v", err)
	}
	service, err := provider.New(os.Getenv("VASTPLAN_ARTIFACT_FILE_PROVIDER_ROOT"))
	if err != nil {
		log.Fatalf("初始化本地文件制品存储 Provider: %v", err)
	}
	volumeID := startup.VolumeID
	if volumeID == "" {
		volumeID = "repository.primary"
	}
	if _, err := service.Provision(volumeID); err != nil {
		log.Fatalf("供给默认本地文件制品卷: %v", err)
	}
	plugin := sdk.New(provider.PluginID, provider.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("本地文件制品存储 Provider 退出: %v", err)
	}
}
