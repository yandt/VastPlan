// Command platformadminaccesspolicy starts the platform administration policy.
package main

import (
	"context"
	"encoding/json"
	"log"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	policy "cdsoft.com.cn/VastPlan/extensions/plugins/com.vastplan.foundation.security.platform-admin-access-policy/policy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	p := sdk.New(policy.PluginID, policy.PluginVersion, map[string]string{"backend": "^1.0"})
	descriptor, _ := json.Marshal(extpoint.CheckerDescriptor{Title: "平台管理角色访问策略", Applies: &extpoint.Applies{}})
	p.Contribute(sdk.Contribution{ExtensionPoint: extpoint.PermissionChecker, ID: policy.Capability, Priority: 1000, Descriptor: descriptor, Handlers: map[string]sdk.Handler{"check": func(ctx context.Context, _ sdk.Host, c *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
		return policy.Check(ctx, c, raw)
	}}})
	if err := p.Serve(); err != nil {
		log.Fatal(err)
	}
}
