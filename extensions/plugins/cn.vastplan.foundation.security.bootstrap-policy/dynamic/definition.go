package main

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/dynamicgo"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	bootstrappolicy "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.bootstrap-policy/policy"
)

// definition 是 dynamic-go 模块唯一的 protocolbus 适配，避免内核编译任何具体插件代码。
func definition() dynamicgo.Plugin {
	return dynamicgo.Plugin{
		ID: bootstrappolicy.PluginID, Version: bootstrappolicy.PluginVersion,
		Contributions: []dynamicgo.Contribution{
			{
				ExtensionPoint: extpoint.PermissionChecker, ID: bootstrappolicy.WriteGuardID, Priority: 1_000_000,
				Descriptor: bootstrappolicy.CheckerDescriptor("系统设置写保护"),
				Handlers:   map[string]dynamicgo.Handler{"check": adapt(bootstrappolicy.WriteGuard)},
			},
			{
				ExtensionPoint: extpoint.PermissionChecker, ID: bootstrappolicy.BaselineID, Priority: -1_000_000,
				Descriptor: bootstrappolicy.CheckerDescriptor("系统设置自举权限基线"),
				Handlers:   map[string]dynamicgo.Handler{"check": adapt(bootstrappolicy.Baseline)},
			},
		},
	}
}

type policyHandler func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)

func adapt(handler policyHandler) dynamicgo.Handler {
	return func(ctx context.Context, _ dynamicgo.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		return handler(ctx, callCtx, payload)
	}
}
