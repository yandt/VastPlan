// Command artifactassessmentdatabasefile materializes a pinned local Trivy DB.
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.assessment.database.file/snapshot"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var config snapshot.Config
	if err := sdk.DecodeStartupConfiguration(&config); err != nil {
		log.Fatalf("读取 Trivy database file snapshot 配置: %v", err)
	}
	materializer, err := snapshot.New(config)
	if err != nil {
		log.Fatalf("初始化 Trivy database file snapshot: %v", err)
	}
	plugin := sdk.New(snapshot.PluginID, snapshot.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(materializer.Contribution())
	plugin.OnLifecycle(materializer.Lifecycle())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Trivy database file snapshot 退出: %v", err)
	}
}
