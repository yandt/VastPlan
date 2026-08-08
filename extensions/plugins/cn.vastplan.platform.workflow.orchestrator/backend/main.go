// Command workflow-orchestrator starts the durable workflow plugin.
package main

import (
	"log"

	workflow "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.workflow.orchestrator/workfloworchestrator"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	service := workflow.New()
	plugin := sdk.New(workflow.PluginID, workflow.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(workflow.Contribution(service))
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Workflow Orchestrator 插件退出: %v", err)
	}
}
