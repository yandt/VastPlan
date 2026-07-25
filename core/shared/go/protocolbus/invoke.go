package protocolbus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocollimit"
)

func (h *Host) Invoke(ctx context.Context, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (response *pluginhostv1.InvokeResponse, invokeErr error) {
	if target == nil || target.ExtensionPoint == "" || target.Capability == "" {
		return errorResponse(errorcode.WireInvalidRequest, "调用目标不能为空", false), nil
	}
	limits := h.limits()
	if !limits.PayloadAllowed(payload) {
		return errorResponse(errorcode.PayloadTooLarge,
			fmt.Sprintf("payload 为 %d bytes，超过上限 %d bytes", len(payload), limits.MaxPayloadBytes), false), nil
	}
	provenance := callcontext.Provenance{Source: "protocolbus.public", AuthenticatedBy: "trusted-host-api"}
	if inherited, ok := callcontext.FromContext(ctx); ok {
		provenance = inherited.Provenance()
	}
	trusted, err := callcontext.ValidateIngress(callCtx, provenance)
	if err != nil {
		return errorResponse(errorcode.WireInvalidRequest, err.Error(), false), nil
	}
	callCtx = trusted.Wire()
	ctx = callcontext.WithTrusted(ctx, trusted)
	ctx, callCtx, cancel := boundedCallContext(ctx, callCtx, limits)
	defer cancel()
	if code, message := appendCallTarget(callCtx, target, limits.MaxCallDepth); code != "" {
		return errorResponse(code, message, false), nil
	}
	if !limits.MetadataAllowed(proto.Size(callCtx)) {
		return errorResponse(errorcode.MetadataTooLarge,
			fmt.Sprintf("CallContext 为 %d bytes，超过 metadata 上限 %d bytes", proto.Size(callCtx), limits.MaxMetadataBytes), false), nil
	}
	if err := h.enterCall(); err != nil {
		// Drain 是宿主的可用状态，不是 wire 故障；调用方应得到可重试的应用层结论，
		// 才能按正常路由切到候选实例，而不是把它误报为网络中断。
		code := errorcode.PluginInactive
		if errors.Is(err, ErrConcurrencyLimited) {
			code = errorcode.ConcurrencyLimited
		}
		return errorResponse(code, err.Error(), true), nil
	}
	defer h.leaveCall()

	if h.Observer != nil {
		var finish func(string, error)
		callCtx, finish = h.Observer.BeginCall(ctx, callCtx, "protocolbus.invoke", map[string]string{
			"extension_point": target.ExtensionPoint,
		})
		defer func() {
			status := "transport_error"
			if invokeErr == nil && response != nil && response.Result != nil {
				status = response.Result.Status.String()
			}
			finish(status, invokeErr)
		}()
	}

	// 1) before 钩子：限流/配额等可在此否决
	if err := h.runBeforeHooks(ctx, extpoint.PointInvoke, callCtx, target); err != nil {
		var abort *HookAbort
		if errors.As(err, &abort) {
			h.Logf("调用被钩子否决 %s/%s：%s（由 %q）",
				target.ExtensionPoint, target.Capability, abort.Reason, abort.HookID)
			return errorResponse(errorcode.HookAborted, abort.Reason, false), nil
		}
		return nil, err
	}

	// 2) 权限判定
	if res := h.CheckPermission(ctx, callCtx, target); !res.Allowed() {
		h.Logf("权限拒绝 %s/%s：%s（由 %q 判定）",
			target.ExtensionPoint, target.Capability, res.Reason, res.DecidedBy)
		return errorResponse(errorcode.PermissionDenied, res.Reason, false), nil
	}

	// 3) 分发
	ctx = context.WithValue(ctx, currentDispatchTargetKey{}, callTargetKey(target))
	resp, err := h.invoke(ctx, target, callCtx, payload)
	if err != nil {
		if errors.Is(err, errPendingQueueFull) {
			return errorResponse(errorcode.QueueFull, err.Error(), true), nil
		}
		return nil, err // 传输层失败：无结论可供 after 钩子观察
	}
	if resp != nil && !limits.PayloadAllowed(resp.Payload) {
		return errorResponse(errorcode.PayloadTooLarge,
			fmt.Sprintf("响应 payload 为 %d bytes，超过上限 %d bytes", len(resp.Payload), limits.MaxPayloadBytes), false), nil
	}

	// 4) after 钩子：计量/审计等只观察，不改变结论
	h.runAfterHooks(ctx, extpoint.PointInvoke, callCtx, target, resp.Result)
	return resp, nil
}

// appendCallTarget 在公开调用入口维护能力调用链。CallContext 已由
// boundedCallContext 克隆，因此这里不会修改调用方持有的对象。
func appendCallTarget(callCtx *contractv1.CallContext, target *contractv1.CallTarget, maxDepth int) (string, string) {
	key := callTargetKey(target)
	for _, ancestor := range callCtx.CallPath {
		if ancestor == key {
			return errorcode.CallCycleDetected,
				fmt.Sprintf("检测到能力调用环：%s -> %s", strings.Join(callCtx.CallPath, " -> "), key)
		}
	}
	if len(callCtx.CallPath) >= maxDepth {
		return errorcode.CallDepthExceeded, fmt.Sprintf("能力调用深度达到上限 %d", maxDepth)
	}
	callCtx.CallPath = append(callCtx.CallPath, key)
	return "", ""
}

func callTargetKey(target *contractv1.CallTarget) string {
	key := target.ExtensionPoint + "/" + target.Capability
	if operation := target.GetOperation(); operation != "" {
		key += "#" + operation
	}
	if instanceID := target.GetInstanceId(); instanceID != "" {
		key += "@" + instanceID
	}
	return key
}

// invoke 内部分发：**不做权限判定**。
//
// 供两处使用：① 权限校验器自身的调用（否则自我递归）；② 事件扇出（内核发起的投递，
// 非用户调用）。业务路径一律走 Invoke。
func (h *Host) invoke(ctx context.Context, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*pluginhostv1.InvokeResponse, error) {

	c, ok := h.Registry.Lookup(target.ExtensionPoint, target.Capability)
	if !ok {
		h.mu.RLock()
		forwarder := h.forwarder
		h.mu.RUnlock()
		if forwarder == nil {
			return nil, fmt.Errorf("能力未注册：%s/%s", target.ExtensionPoint, target.Capability)
		}
		// Invoke already appended this target for local hook/authorization. The
		// destination Host runs its own public Invoke pipeline and must append it
		// exactly once there, otherwise the same call is mistaken for a cycle.
		forwarded := proto.Clone(callCtx).(*contractv1.CallContext)
		key, appendedByPublicInvoke := ctx.Value(currentDispatchTargetKey{}).(string)
		if last := len(forwarded.CallPath) - 1; appendedByPublicInvoke && last >= 0 && key == callTargetKey(target) && forwarded.CallPath[last] == key {
			forwarded.CallPath = forwarded.CallPath[:last]
		}
		result, payload, err := forwarder(ctx, target, forwarded, payload)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, errors.New("跨服务能力返回空 CallResult")
		}
		return &pluginhostv1.InvokeResponse{Result: result, Payload: payload}, nil
	}

	// 内核自身提供的能力：直接本地调用，不经流
	if c.PluginID == KernelPluginID {
		return h.callHostService(ctx, target, callCtx, payload)
	}

	h.mu.RLock()
	embedded := h.embeddedByPlugin[c.PluginID]
	sess, ok := h.byPlugin[c.PluginID]
	h.mu.RUnlock()
	if embedded != nil {
		return embedded.invoke(ctx, h, target, callCtx, payload)
	}
	if !ok {
		return nil, fmt.Errorf("能力 %s 的提供者 %s 当前未接入", target.Capability, c.PluginID)
	}
	return h.invokeOn(ctx, sess, target, callCtx, payload)
}

// invokeOn 在指定会话上发起一次调用并等待响应。
func (h *Host) invokeOn(ctx context.Context, sess *session, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*pluginhostv1.InvokeResponse, error) {

	reqID := sess.nextRequestID()
	forwardedCallCtx, err := projectContextForPlugin(callCtx, target, sess.policy)
	if err != nil {
		return nil, err
	}
	delegationToken, forwardedCallCtx := sess.issueDelegation(forwardedCallCtx)
	defer sess.releaseDelegation(delegationToken)
	ch, err := sess.await(reqID, h.limits().MaxPendingRequests)
	if err != nil {
		return nil, err
	}
	defer sess.release(reqID)

	if err := sess.send(&pluginhostv1.FromHost{
		Msg: &pluginhostv1.FromHost_Invoke{
			Invoke: &pluginhostv1.InvokeRequest{
				RequestId: reqID, Target: target, Context: forwardedCallCtx, Payload: payload,
				DelegationToken: &delegationToken,
			},
		},
	}); err != nil {
		return nil, fmt.Errorf("下发调用失败: %w", err)
	}

	select {
	case msg, ok := <-ch:
		if !ok {
			// 通道被关闭 = 插件失联，在途调用立刻脱身而非挂到超时
			return nil, fmt.Errorf("插件 %s 已失联: %w", sess.pluginID, sess.err())
		}
		return msg.GetInvokeResult(), nil
	case <-ctx.Done():
		if sess.hasFeature(protocol.FeatureCancellation) {
			_ = sess.send(&pluginhostv1.FromHost{Msg: &pluginhostv1.FromHost_Cancel{
				Cancel: &pluginhostv1.Cancel{RequestId: reqID},
			}})
		}
		return nil, ctx.Err()
	}
}

func boundedCallContext(ctx context.Context, callCtx *contractv1.CallContext, limits protocollimit.Limits) (context.Context, *contractv1.CallContext, context.CancelFunc) {
	limits = limits.Normalize()
	deadline := time.Now().Add(limits.DefaultDeadline)
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}
	if callCtx != nil && callCtx.DeadlineUnixMs != nil {
		declared := time.UnixMilli(*callCtx.DeadlineUnixMs)
		if declared.Before(deadline) {
			deadline = declared
		}
	}
	bounded := &contractv1.CallContext{}
	if callCtx != nil {
		bounded = proto.Clone(callCtx).(*contractv1.CallContext)
	}
	deadlineUnixMs := deadline.UnixMilli()
	bounded.DeadlineUnixMs = &deadlineUnixMs
	boundedCtx, cancel := context.WithDeadline(ctx, deadline)
	return boundedCtx, bounded, cancel
}

// serveHostCall 处理插件的回调：本地命中即内核服务，否则转给提供该能力的插件
// （即插件→插件也只经 capability 寻址，不得互相 import——见工程规范 §七）。
