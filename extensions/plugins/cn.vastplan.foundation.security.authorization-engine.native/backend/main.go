// Package main exposes the default Go authorization.engine.v1 Provider.
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authorization-engine.native/nativeengine"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	pluginID      = "cn.vastplan.foundation.security.authorization-engine.native"
	pluginVersion = "0.1.1"
)

func main() {
	plugin := sdk.New(pluginID, pluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(nativeengine.NewEngine().Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Native Authorization Engine 退出: %v", err)
	}
}
