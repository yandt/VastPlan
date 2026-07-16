// 示例权限策略插件的 backend 面。
//
// 演示 **select 分发**（§4.2）：贡献两个不同优先级的校验器，宿主按 priority 高→低
// 逐个问，遇到第一个非 abstain 即定论。
//
//	demo.denylist (priority 100)：命中拒绝名单 → deny；否则 abstain（交给下一个）
//	demo.baseline (priority  10)：admin/agent/system/plugin → allow；其余 abstain
//	两者都 abstain → 宿主 fail-closed 拒绝（ADR-0021）
//
// 权限是插件而非内核内置——这正是"一切功能皆插件"（ADR-0001）。
package main

import (
	"context"
	"encoding/json"
	"log"

	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
)

// deniedCapabilities 拒绝名单：无论谁调用都不放行。
var deniedCapabilities = map[string]string{
	"vastplan.forbidden": "该能力在拒绝名单中",
}

func main() {
	p := sdk.New("com.vastplan.demo-permission", "0.1.0", map[string]string{"backend": "^0.1"})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.PermissionChecker,
		ID:             "demo.denylist",
		Priority:       100, // 高优先级：先于基线策略被问
		Descriptor:     mustJSON(extpoint.CheckerDescriptor{Title: "拒绝名单（高优先级）"}),
		Handlers:       map[string]sdk.Handler{"check": denylist},
	})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.PermissionChecker,
		ID:             "demo.baseline",
		Priority:       10, // 低优先级：仅当上面弃权才被问
		Descriptor:     mustJSON(extpoint.CheckerDescriptor{Title: "基线策略（低优先级）"}),
		Handlers:       map[string]sdk.Handler{"check": baseline},
	})

	if err := p.Serve(); err != nil {
		log.Fatalf("插件退出: %v", err)
	}
}

// denylist 只对名单内的能力表态；其余一律弃权，交给优先级更低的校验器。
func denylist(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	var req extpoint.PermissionRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, nil, err
	}
	if reason, hit := deniedCapabilities[req.Capability]; hit {
		return decide(extpoint.DecisionDeny, reason)
	}
	return decide(extpoint.DecisionAbstain, "")
}

// baseline 基线策略：按三元组的 caller 维度判定。
// 注意它只看得到 CallContext——那是**被检查调用**的真实身份与场景（宿主原样透传）。
func baseline(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if callCtx == nil || callCtx.Caller == nil {
		return decide(extpoint.DecisionAbstain, "") // 身份不明 → 不表态，最终 fail-closed
	}
	if callCtx.Principal.GetIsAdmin() {
		return decide(extpoint.DecisionAllow, "管理员放行")
	}
	switch callCtx.Caller.Kind {
	case contractv1.CallerKind_CALLER_KIND_AGENT,
		contractv1.CallerKind_CALLER_KIND_SYSTEM,
		contractv1.CallerKind_CALLER_KIND_PLUGIN:
		return decide(extpoint.DecisionAllow, "可信调用方放行")
	default:
		return decide(extpoint.DecisionAbstain, "")
	}
}

func decide(d extpoint.Decision, reason string) (*contractv1.CallResult, []byte, error) {
	out, err := json.Marshal(extpoint.PermissionResponse{Decision: d, Reason: reason})
	if err != nil {
		return nil, nil, err
	}
	return sdk.OK(0), out, nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // 常量级 descriptor 序列化失败属编码错误，应在开发期立即暴露
	}
	return b
}
