// Package main exposes the default approval.policy.v2 Provider.
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.approval-policy.native/nativepolicy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	pluginID      = "cn.vastplan.foundation.security.approval-policy.native"
	pluginVersion = "0.1.2"
)

func main() {
	profiles, err := loadProfiles()
	if err != nil {
		log.Fatalf("加载 Native Approval Profiles: %v", err)
	}
	provider, err := nativepolicy.New(profiles)
	if err != nil {
		log.Fatalf("初始化 Native Approval Provider: %v", err)
	}
	plugin := sdk.New(pluginID, pluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(nativepolicy.AccessPolicyContribution())
	plugin.Contribute(provider.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Native Approval Provider 退出: %v", err)
	}
}
