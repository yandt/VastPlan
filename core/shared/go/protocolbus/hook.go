package protocolbus

import (
	"context"
	"encoding/json"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
)

// HookAbort 某个 before 钩子否决了本次调用。
type HookAbort struct {
	HookID string
	Reason string
}

func (a *HookAbort) Error() string {
	return fmt.Sprintf("被钩子 %s 否决: %s", a.HookID, a.Reason)
}

// runBeforeHooks 按 priority 高→低**顺序**执行 before 钩子；任一钩子否决即中止（一票否决）。
//
// 与 event.sink 的并行扇出不同：钩子是链式中间件，顺序与否决是其本质
// （限流/配额等横切关注点靠它落地，且它们是插件而非内核内置——ADR-0001）。
//
// 钩子自身不可达/报错 → **记录并放行**：钩子是横切增强，不该因它挂了就让整个系统不可用。
// 这与 permission.checker 的"不可达即判拒"刻意相反——权限是授权依据，钩子不是。
func (h *Host) runBeforeHooks(ctx context.Context, point string,
	callCtx *contractv1.CallContext, target *contractv1.CallTarget) error {

	hooks := h.hooksAt(point, extpoint.PhaseBefore)
	if len(hooks) == 0 {
		return nil
	}

	req := extpoint.HookRequest{
		Point: point, Phase: extpoint.PhaseBefore,
		Target: targetRef(target),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil // 序列化失败属编码错误，不该拖垮调用；下方 invoke 失败也会记录
	}

	for _, hk := range hooks {
		resp, err := h.callHook(ctx, hk, callCtx, payload)
		if err != nil {
			h.Logf("before 钩子 %s 不可达，已放行: %v", hk.ID, err)
			continue // 钩子挂了不阻断业务
		}
		if resp.Abort {
			return &HookAbort{HookID: hk.ID, Reason: resp.Reason}
		}
	}
	return nil
}

// runAfterHooks 按 priority 高→低顺序执行 after 钩子；**只观察，不改变调用结论**。
func (h *Host) runAfterHooks(ctx context.Context, point string,
	callCtx *contractv1.CallContext, target *contractv1.CallTarget, result *contractv1.CallResult) {

	hooks := h.hooksAt(point, extpoint.PhaseAfter)
	if len(hooks) == 0 {
		return
	}

	req := extpoint.HookRequest{
		Point: point, Phase: extpoint.PhaseAfter,
		Target: targetRef(target),
		Result: &extpoint.HookResult{
			Status:     result.Status.String(),
			ErrorCode:  result.GetError().GetCode(),
			DurationMs: result.GetUsage().GetDurationMs(),
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return
	}

	for _, hk := range hooks {
		if _, err := h.callHook(ctx, hk, callCtx, payload); err != nil {
			h.Logf("after 钩子 %s 执行失败（不影响调用结论）: %v", hk.ID, err)
		}
	}
}

// hooksAt 取挂在指定钩子位与阶段的贡献（已按 priority 降序）。
func (h *Host) hooksAt(point string, phase extpoint.Phase) []registry.Contribution {
	all := h.Registry.List(extpoint.Hook)
	out := make([]registry.Contribution, 0, len(all))
	for _, c := range all {
		d, err := extpoint.ParseHook(c.Descriptor)
		if err != nil {
			h.Logf("钩子 %s 的 descriptor 非法，已跳过: %v", c.ID, err)
			continue
		}
		if d.Matches(point, phase) {
			out = append(out, c)
		}
	}
	return out
}

// callHook 调用一个钩子并解析其回答。走内部 invoke：钩子调用本身不得再触发钩子/权限，
// 否则无限递归。
func (h *Host) callHook(ctx context.Context, hk registry.Contribution,
	callCtx *contractv1.CallContext, payload []byte) (*extpoint.HookResponse, error) {

	op := "run"
	resp, err := h.invoke(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.Hook,
		Capability:     hk.ID,
		Operation:      &op,
	}, callCtx, payload)
	if err != nil {
		return nil, err
	}
	if resp.Result.Status != contractv1.CallResult_STATUS_OK {
		return nil, fmt.Errorf("钩子返回错误: %s", resp.Result.Error.GetMessage())
	}

	out := &extpoint.HookResponse{}
	if len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, out); err != nil {
			return nil, fmt.Errorf("钩子回答无法解析: %w", err)
		}
	}
	return out, nil
}

func targetRef(t *contractv1.CallTarget) extpoint.Target {
	return extpoint.Target{
		ExtensionPoint: t.ExtensionPoint,
		Capability:     t.Capability,
		Operation:      t.GetOperation(),
	}
}
