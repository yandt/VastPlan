package protocolbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/registry"
)

// ── select 语义：permission.checker（§4.2/§4.3）──────────

// PermissionResult 一次权限判定的结论与由来。
type PermissionResult struct {
	Decision extpoint.Decision
	// DecidedBy 做出定论的校验器 id；无人定论时为空。
	DecidedBy string
	Reason    string
}

// Allowed 是否放行。
func (r PermissionResult) Allowed() bool { return r.Decision == extpoint.DecisionAllow }

// CheckPermission 按 select 语义判定 (caller, scene, target) 三元组：
// 按 priority 高→低逐个问，**遇到第一个非 abstain 即定论**；
// 全部 abstain（含一个校验器都没注册）→ **fail-closed 拒绝**。
//
// 零校验器即拒绝是有意为之：权限校验器是插件（ADR-0001 一切功能皆插件），
// 没装它就等于没有授权依据——此时放行会让"忘装权限插件"变成静默的全开放。
func (h *Host) CheckPermission(ctx context.Context, callCtx *contractv1.CallContext,
	target *contractv1.CallTarget) PermissionResult {

	checkers := h.Registry.List(extpoint.PermissionChecker) // 已按 priority 降序

	callerKind := ""
	if callCtx != nil && callCtx.Caller != nil {
		callerKind = callCtx.Caller.Kind.String()
	}
	scene := ""
	if callCtx != nil {
		scene = callCtx.Scene
	}

	req, _ := json.Marshal(extpoint.PermissionRequest{
		ExtensionPoint: target.ExtensionPoint,
		Capability:     target.Capability,
		Operation:      target.GetOperation(),
	})

	for _, c := range checkers {
		desc, err := extpoint.ParseChecker(c.Descriptor)
		if err != nil {
			// descriptor 坏了 → 跳过它，但必须留痕：坏的策略不该静默失效
			h.Logf("权限校验器 %s 的 descriptor 非法，已跳过: %v", c.ID, err)
			continue
		}
		if !desc.Matches(callerKind, scene, target.Capability) {
			continue // 预筛不中，不必往返
		}

		op := "check"
		resp, err := h.invoke(ctx, &contractv1.CallTarget{
			ExtensionPoint: extpoint.PermissionChecker,
			Capability:     c.ID,
			Operation:      &op,
		}, callCtx, req)
		if err != nil {
			// 校验器不可达 → 不得当作放行（fail-closed）
			h.Logf("权限校验器 %s 不可达，视为拒绝: %v", c.ID, err)
			return PermissionResult{Decision: extpoint.DecisionDeny, DecidedBy: c.ID,
				Reason: fmt.Sprintf("校验器不可达: %v", err)}
		}
		if resp.Result.Status != contractv1.CallResult_STATUS_OK {
			h.Logf("权限校验器 %s 返回错误，视为拒绝: %s", c.ID, resp.Result.Error.GetMessage())
			return PermissionResult{Decision: extpoint.DecisionDeny, DecidedBy: c.ID,
				Reason: "校验器内部错误: " + resp.Result.Error.GetMessage()}
		}

		var out extpoint.PermissionResponse
		if err := json.Unmarshal(resp.Payload, &out); err != nil {
			h.Logf("权限校验器 %s 的回答无法解析，视为拒绝: %v", c.ID, err)
			return PermissionResult{Decision: extpoint.DecisionDeny, DecidedBy: c.ID,
				Reason: "校验器回答无法解析"}
		}

		if out.Decision == extpoint.DecisionAbstain {
			continue // 弃权 → 问下一个（优先级更低者）
		}
		// 第一个非 abstain 即定论
		return PermissionResult{Decision: out.Decision, DecidedBy: c.ID, Reason: out.Reason}
	}

	return PermissionResult{
		Decision: extpoint.DecisionDeny,
		Reason:   fmt.Sprintf("无校验器给出结论（共 %d 个），按 fail-closed 拒绝", len(checkers)),
	}
}

// ── fanout 语义：event.sink（§4.2/§4.3）─────────────────

// SinkOutcome 一个事件汇的处理结果。
type SinkOutcome struct {
	SinkID string
	Err    error
}

// PublishEvent 按 fanout 语义把事件投递给所有**订阅了该类型**的 event.sink。
//
// **并行投递、不保证顺序**：事件汇是彼此独立的消费者，隔离与延迟优先于排序——
// 一个慢汇（如写远端存储）不该拖住其余。需要顺序或需要否决调用的是 hook，不是 event.sink。
//
// 失败隔离：某个汇失败不影响其余——审计插件挂了不该连带可观测插件一起哑火。
// 返回逐个结果（顺序与匹配顺序一致，便于确定性报告），调用方可据此告警。
func (h *Host) PublishEvent(ctx context.Context, event *contractv1.CallEvent) []SinkOutcome {
	sinks := h.Registry.List(extpoint.EventSink) // List 已排序，此处仅用于确定性报告

	matched := make([]registry.Contribution, 0, len(sinks))
	for _, s := range sinks {
		desc, err := extpoint.ParseSink(s.Descriptor)
		if err != nil {
			h.Logf("事件汇 %s 的 descriptor 非法，已跳过: %v", s.ID, err)
			continue
		}
		if desc.Subscribes(event.Type) {
			matched = append(matched, s)
		}
	}
	if len(matched) == 0 {
		return nil
	}

	payload, err := json.Marshal(eventPayload{
		ID: event.Id, Type: event.Type, Source: event.Source,
		Subject: event.GetSubject(), TenantID: event.TenantId, Data: event.Payload,
	})
	if err != nil {
		h.Logf("事件序列化失败: %v", err)
		return []SinkOutcome{{Err: err}}
	}

	// 事件本身携带身份/租户/追踪，构造一个以内核为发起方的上下文投递
	callCtx := &contractv1.CallContext{
		Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: h.KernelName},
		Scene:    "kernel.event_dispatch",
		TenantId: event.TenantId,
		Trace:    event.Trace,
	}

	outcomes := make([]SinkOutcome, len(matched))
	var wg sync.WaitGroup
	for i, s := range matched {
		wg.Add(1)
		go func(i int, s registry.Contribution) {
			defer wg.Done()
			op := "consume"
			resp, err := h.invoke(ctx, &contractv1.CallTarget{
				ExtensionPoint: extpoint.EventSink,
				Capability:     s.ID,
				Operation:      &op,
			}, callCtx, payload)

			out := SinkOutcome{SinkID: s.ID}
			switch {
			case err != nil:
				out.Err = err
			case resp.Result.Status != contractv1.CallResult_STATUS_OK:
				out.Err = fmt.Errorf("%s: %s", resp.Result.Error.GetCode(), resp.Result.Error.GetMessage())
			}
			if out.Err != nil {
				// 记录但不中断其余：失败隔离
				h.Logf("事件汇 %s 处理 %s 失败: %v", s.ID, event.Type, out.Err)
			}
			outcomes[i] = out
		}(i, s)
	}
	wg.Wait()
	return outcomes
}

// eventPayload 投递给事件汇的 JSON 形态（CallEvent 的可读投影）。
type eventPayload struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Source   string `json:"source"`
	Subject  string `json:"subject,omitempty"`
	TenantID string `json:"tenantId"`
	Data     []byte `json:"data,omitempty"`
}
