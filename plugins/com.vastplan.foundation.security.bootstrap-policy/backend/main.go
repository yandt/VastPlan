// bootstrap-policy 是平台设置服务之前启动的最小权限基线。
package main

import (
	"log"

	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
)

const (
	pluginID      = "com.vastplan.foundation.security.bootstrap-policy"
	pluginVersion = "0.1.0"
	writeGuardID  = "foundation.security.bootstrap-policy.write-guard"
	baselineID    = "foundation.security.bootstrap-policy.baseline"
)

func main() {
	p := sdk.New(pluginID, pluginVersion, map[string]string{"backend": "^1.0"})
	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.PermissionChecker,
		ID:             writeGuardID,
		Priority:       1_000_000,
		Descriptor:     checkerDescriptor("系统设置写保护"),
		Handlers:       map[string]sdk.Handler{"check": writeGuard},
	})
	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.PermissionChecker,
		ID:             baselineID,
		Priority:       -1_000_000,
		Descriptor:     checkerDescriptor("系统设置自举权限基线"),
		Handlers:       map[string]sdk.Handler{"check": baseline},
	})
	if err := p.Serve(); err != nil {
		log.Fatalf("自举权限插件退出: %v", err)
	}
}
