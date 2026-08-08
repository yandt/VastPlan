// Command navigationorganizer starts the service-scoped Portal navigation organizer.
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.application.portal.navigation-organizer/navigationorganizer"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	plugin := sdk.New(navigationorganizer.PluginID, navigationorganizer.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(navigationorganizer.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Portal navigation organizer exited: %v", err)
	}
}
