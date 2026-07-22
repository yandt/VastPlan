// Package main exposes the default Go authorization.engine.v1 Provider.
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/sdk/go/authorizationnative"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	pluginID      = "cn.vastplan.foundation.security.authorization-engine.native"
	pluginVersion = "0.1.0"
)

func main() {
	plugin := sdk.New(pluginID, pluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(authorizationnative.NewEngine().Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Native Authorization Engine 退出: %v", err)
	}
}
