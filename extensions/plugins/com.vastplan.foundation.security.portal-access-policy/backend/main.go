// Command portalaccesspolicy starts the Portal access-policy plugin process.
package main

import (
	"context"
	"encoding/json"
	"log"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	policy "cdsoft.com.cn/VastPlan/extensions/plugins/com.vastplan.foundation.security.portal-access-policy/policy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	p := sdk.New(policy.PluginID, policy.PluginVersion, map[string]string{"backend": "^0.1"})
	d, _ := json.Marshal(extpoint.CheckerDescriptor{Title: "门户角色访问策略", Applies: &extpoint.Applies{}})
	p.Contribute(sdk.Contribution{ExtensionPoint: extpoint.PermissionChecker, ID: policy.Capability, Priority: 1000, Descriptor: d, Handlers: map[string]sdk.Handler{"check": func(ctx context.Context, _ sdk.Host, c *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
		return policy.Check(ctx, c, raw)
	}}})
	if err := p.Serve(); err != nil {
		log.Fatal(err)
	}
}
