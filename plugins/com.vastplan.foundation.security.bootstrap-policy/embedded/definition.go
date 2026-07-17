// Package bootstrapembedded 是 bootstrap-policy 对 protocolbus 内嵌 ABI 的唯一适配。
// 静态组合与 dynamic-go .so 共用它，避免贡献 descriptor 或 handler 路由漂移。
package bootstrapembedded

import (
	"context"

	bootstrappolicy "cdsoft.com.cn/VastPlan/plugins/com.vastplan.foundation.security.bootstrap-policy/policy"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
)

func Definition() protocolbus.EmbeddedPlugin {
	return protocolbus.EmbeddedPlugin{
		ID: bootstrappolicy.PluginID, Version: bootstrappolicy.PluginVersion,
		Contributions: []protocolbus.EmbeddedContribution{
			{
				ExtensionPoint: extpoint.PermissionChecker, ID: bootstrappolicy.WriteGuardID, Priority: 1_000_000,
				Descriptor: bootstrappolicy.CheckerDescriptor("系统设置写保护"),
				Handlers:   map[string]protocolbus.EmbeddedHandler{"check": adapt(bootstrappolicy.WriteGuard)},
			},
			{
				ExtensionPoint: extpoint.PermissionChecker, ID: bootstrappolicy.BaselineID, Priority: -1_000_000,
				Descriptor: bootstrappolicy.CheckerDescriptor("系统设置自举权限基线"),
				Handlers:   map[string]protocolbus.EmbeddedHandler{"check": adapt(bootstrappolicy.Baseline)},
			},
		},
	}
}

type policyHandler func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)

func adapt(handler policyHandler) protocolbus.EmbeddedHandler {
	return func(ctx context.Context, _ protocolbus.EmbeddedHost, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		return handler(ctx, callCtx, payload)
	}
}
