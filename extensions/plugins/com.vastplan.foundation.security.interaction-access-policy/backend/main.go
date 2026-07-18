// Package main serves the interaction entry access policy as a process plugin.
package main

import (
	"context"
	"encoding/json"
	"log"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	policy "cdsoft.com.cn/VastPlan/extensions/plugins/com.vastplan.foundation.security.interaction-access-policy/policy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	p := sdk.New(policy.PluginID, policy.PluginVersion, map[string]string{"backend": "^0.1"})
	d, _ := json.Marshal(extpoint.CheckerDescriptor{Title: "交互入口访问策略", Applies: &extpoint.Applies{}})
	p.Contribute(sdk.Contribution{ExtensionPoint: extpoint.PermissionChecker, ID: policy.Capability, Priority: 1000, Descriptor: d, Handlers: map[string]sdk.Handler{"check": func(ctx context.Context, _ sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		return policy.Check(ctx, callCtx, payload)
	}}})
	if err := p.Serve(); err != nil {
		log.Fatalf("交互访问策略插件退出: %v", err)
	}
}
