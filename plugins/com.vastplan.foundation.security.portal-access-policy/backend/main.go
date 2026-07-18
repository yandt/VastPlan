// Command portalaccesspolicy starts the Portal access-policy plugin process.
package main

import (
	"context"
	"encoding/json"
	"log"

	policy "cdsoft.com.cn/VastPlan/plugins/com.vastplan.foundation.security.portal-access-policy/policy"
	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
)

func main() {
	p := sdk.New(policy.PluginID, policy.PluginVersion, map[string]string{"backend": "^1.0"})
	d, _ := json.Marshal(extpoint.CheckerDescriptor{Title: "门户角色访问策略", Applies: &extpoint.Applies{}})
	p.Contribute(sdk.Contribution{ExtensionPoint: extpoint.PermissionChecker, ID: policy.Capability, Descriptor: d, Handlers: map[string]sdk.Handler{"check": func(ctx context.Context, _ sdk.Host, c *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
		return policy.Check(ctx, c, raw)
	}}})
	if err := p.Serve(); err != nil {
		log.Fatal(err)
	}
}
