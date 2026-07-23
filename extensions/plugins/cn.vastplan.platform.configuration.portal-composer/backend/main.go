// Command portalcomposer starts the Portal Composer backend plugin. Deployment
// configuration and artifact trust remain host capabilities, not environment
// variables accepted by this process.
package main

import (
	"log"

	portalcomposer "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.configuration.portal-composer/portalcomposer"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	service := portalcomposer.New(nil)
	p := sdk.New(portalcomposer.PluginID, portalcomposer.PluginVersion, map[string]string{"backend": "^0.1"})
	p.Contribute(portalcomposer.Contribution(service))
	p.Contribute(portalcomposer.PreferenceContribution(service))
	if err := p.Serve(); err != nil {
		log.Fatalf("Portal Composer 插件退出: %v", err)
	}
}
