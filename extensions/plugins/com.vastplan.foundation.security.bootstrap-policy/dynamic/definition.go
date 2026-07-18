package main

import (
	"context"

	bootstrappolicy "cdsoft.com.cn/VastPlan/extensions/plugins/com.vastplan.foundation.security.bootstrap-policy/policy"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

// definition 是 dynamic-go 模块唯一的 protocolbus 适配，避免内核编译任何具体插件代码。
func definition() protocolbus.EmbeddedPlugin {
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
