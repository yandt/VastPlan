// Package policybundle registers stateless first-party workload policies in a
// single Authorization Enforcer process. Each policy remains an independently
// tested contribution; sharing the signed executable does not merge its rules.
package policybundle

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authorization-enforcer/policybundle/approval"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authorization-enforcer/policybundle/interaction"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authorization-enforcer/policybundle/platformadmin"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authorization-enforcer/policybundle/portal"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type handler func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)

// Register adds all stateless workload policy contributions to one plugin
// instance. The signed manifest carries the same contribution IDs/priorities.
func Register(plugin *sdk.Plugin) {
	register(plugin, platformadmin.Capability, "平台管理 workload 访问策略", 1000, &extpoint.Applies{}, platformadmin.Check)
	register(plugin, portal.Capability, "门户角色访问策略", 1000, &extpoint.Applies{}, portal.Check)
	register(plugin, interaction.Capability, "交互入口访问策略", 1000, &extpoint.Applies{}, interaction.Check)
	register(plugin, approval.Capability, "Approval Provider 插件调用策略", 1000, &extpoint.Applies{Target: "foundation.security.approval-policy"}, approval.Check)
}

func register(plugin *sdk.Plugin, id, title string, priority int32, applies *extpoint.Applies, check handler) {
	descriptor, err := json.Marshal(extpoint.CheckerDescriptor{Title: title, Applies: applies})
	if err != nil {
		panic(err)
	}
	plugin.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.PermissionChecker,
		ID:             id,
		Priority:       priority,
		Descriptor:     descriptor,
		Handlers: map[string]sdk.Handler{"check": func(ctx context.Context, _ sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return check(ctx, callCtx, payload)
		}},
	})
}
