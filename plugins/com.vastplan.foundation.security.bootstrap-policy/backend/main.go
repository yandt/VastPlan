// bootstrap-policy 是平台设置服务之前启动的最小权限基线。
package main

import (
	"context"
	"log"

	bootstrappolicy "cdsoft.com.cn/VastPlan/plugins/com.vastplan.foundation.security.bootstrap-policy/policy"
	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
)

func main() {
	p := sdk.New(bootstrappolicy.PluginID, bootstrappolicy.PluginVersion, map[string]string{"backend": "^1.0"})
	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.PermissionChecker,
		ID:             bootstrappolicy.WriteGuardID,
		Priority:       1_000_000,
		Descriptor:     bootstrappolicy.CheckerDescriptor("系统设置写保护"),
		Handlers:       map[string]sdk.Handler{"check": adapt(bootstrappolicy.WriteGuard)},
	})
	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.PermissionChecker,
		ID:             bootstrappolicy.BaselineID,
		Priority:       -1_000_000,
		Descriptor:     bootstrappolicy.CheckerDescriptor("系统设置自举权限基线"),
		Handlers:       map[string]sdk.Handler{"check": adapt(bootstrappolicy.Baseline)},
	})
	if err := p.Serve(); err != nil {
		log.Fatalf("自举权限插件退出: %v", err)
	}
}

type policyHandler func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)

func adapt(handler policyHandler) sdk.Handler {
	return func(ctx context.Context, _ sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		return handler(ctx, callCtx, payload)
	}
}
