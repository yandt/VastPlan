package protocolbus

import (
	"context"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/errorcode"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/operationfence"
)

func (h *Host) serveHostCall(sess *session, req *pluginhostv1.InvokeRequest) {
	if req == nil || req.Target == nil || req.RequestId == "" {
		h.replyHostCall(sess, "", errorResponse(errorcode.WireInvalidRequest, "HostCall 请求不完整", false))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.callTimeout())
	defer cancel()
	if !sess.beginHostCall(req.RequestId, cancel) {
		h.replyHostCall(sess, req.RequestId, errorResponse(errorcode.WireInvalidRequest,
			"HostCall request_id 重复或会话已结束", false))
		return
	}
	defer sess.endHostCall(req.RequestId)

	h.Logf("插件 %s 回调宿主：%s/%s", sess.pluginID, req.Target.ExtensionPoint, req.Target.Capability)
	callCtx, ok := authenticatedHostCallContext(sess, req.GetDelegationToken(), sess.pluginID, req.Context)
	if !ok {
		h.replyHostCall(sess, req.RequestId, errorResponse(errorcode.PermissionDenied,
			"HostCall 缺少有效的宿主身份委托", false))
		return
	}
	if req.Target.ExtensionPoint == extpoint.KernelService && !sess.grants.allowsKernelService(req.Target.Capability) {
		h.replyHostCall(sess, req.RequestId, errorResponse(errorcode.PermissionDenied,
			"插件未在签名清单中声明该内核服务", false))
		return
	}
	// Host-only runtime identity is attached only for a locally registered
	// kernel service. It is never copied into CallContext or forwarded to a
	// remote capability when the local service is absent.
	h.mu.RLock()
	_, localKernelService := h.services[req.Target.Capability]
	h.mu.RUnlock()
	if !localKernelService && !h.externalHostCallAllowed() {
		h.replyHostCall(sess, req.RequestId, errorResponse(errorcode.PermissionDenied,
			"leader runtime 已失去当前 execution fence", true))
		return
	}
	if req.Target.ExtensionPoint == extpoint.KernelService && localKernelService {
		if trustedCtx, identityErr := withLaunchRuntimeIdentity(ctx, sess.policy); identityErr == nil {
			ctx = trustedCtx
		}
	}
	resp, err := h.Invoke(ctx, req.Target, callCtx, req.Payload)
	if err != nil {
		// 寻址/传输层失败 → 转为应用层错误回给插件，避免它把两类错误混为一谈
		h.replyHostCall(sess, req.RequestId, errorResponse(errorcode.HostCallFailed, err.Error(), false))
		return
	}
	h.replyHostCall(sess, req.RequestId, resp)
}

// externalHostCallAllowed closes the stale-leader window before a plugin call
// leaves its Runtime Host. Downstream append-only/CAS rules remain the final
// fencing layer for a lease loss racing with an already-started request.
func (h *Host) externalHostCallAllowed() bool {
	h.mu.RLock()
	provider := h.fenceProvider
	h.mu.RUnlock()
	if provider == nil {
		return true
	}
	_, current := provider.Current()
	return current
}

func authenticatedPluginContext(sess *session, token, pluginID string) (*contractv1.CallContext, bool) {
	bounded, ok := sess.delegatedContext(token)
	if !ok {
		return nil, false
	}
	bounded.Caller = &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginID}
	return bounded, true
}

func authenticatedHostCallContext(sess *session, token, pluginID string, claimed *contractv1.CallContext) (*contractv1.CallContext, bool) {
	if token != "" {
		return authenticatedPluginContext(sess, token, pluginID)
	}
	if !sess.policy.BackgroundService || sess.policy.AutonomousTenantID == "" || !sess.autonomousActive.Load() {
		return nil, false
	}
	// A mismatched tenant is rejected so configuration bugs and cross-tenant
	// attempts are visible. Every other claimed field is ignored: principal,
	// credentials, metadata and caller can only come from a host delegation.
	if claimed != nil && claimed.GetTenantId() != "" && claimed.GetTenantId() != sess.policy.AutonomousTenantID {
		return nil, false
	}
	return &contractv1.CallContext{
		TenantId: sess.policy.AutonomousTenantID,
		Scene:    "system.background",
		Caller:   &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginID},
	}, true
}

func (h *Host) replyHostCall(sess *session, requestID string, resp *pluginhostv1.InvokeResponse) {
	resp.RequestId = requestID
	if err := sess.send(&pluginhostv1.FromHost{
		Msg: &pluginhostv1.FromHost_HostCallResult{HostCallResult: resp},
	}); err != nil {
		h.Logf("回应插件 HostCall 失败: %v", err)
	}
}

// dispatchHostCall 在创建 goroutine 前先占用固定槽位。即使插件被攻破并高速发包，
// 宿主也不会形成无界 goroutine 队列；满载时返回可重试的应用层错误。
func (h *Host) dispatchHostCall(sess *session, req *pluginhostv1.InvokeRequest) {
	h.callbackMu.Lock()
	if h.callbackSlots == nil {
		h.callbackSlots = make(chan struct{}, h.limits().MaxConcurrentCalls)
	}
	slots := h.callbackSlots
	h.callbackMu.Unlock()
	select {
	case slots <- struct{}{}:
		go func() {
			defer func() { <-slots }()
			h.serveHostCall(sess, req)
		}()
	default:
		requestID := ""
		if req != nil {
			requestID = req.RequestId
		}
		h.replyHostCall(sess, requestID, errorResponse(errorcode.ConcurrencyLimited,
			"宿主 HostCall 并发达到上限", true))
	}
}

// callHostService 调用内核自身提供的能力。
func (h *Host) callHostService(ctx context.Context, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*pluginhostv1.InvokeResponse, error) {

	h.mu.RLock()
	fn, ok := h.services[target.Capability]
	fenceProvider := h.fenceProvider
	h.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("内核能力 %s 无实现", target.Capability)
	}
	if fenceProvider != nil {
		if evidence, current := fenceProvider.Current(); current {
			if fenced, fenceErr := operationfence.WithEvidence(ctx, evidence); fenceErr == nil {
				ctx = fenced
			}
		}
	}
	res, out, err := fn(ctx, callCtx, payload)
	if err != nil {
		return errorResponse(errorcode.KernelServiceError, err.Error(), true), nil
	}
	return &pluginhostv1.InvokeResponse{Result: res, Payload: out}, nil
}

// Close 优雅关闭插件：SHUTDOWN 指令 → 摘除贡献 → 回收进程。
func (h *Host) Close(p *PluginInstance) error {
	if p == nil {
		return nil
	}
	if p.embedded != nil {
		return h.closeEmbedded(p.embedded)
	}
	h.mu.RLock()
	sess, ok := h.sessions[p.SessionID]
	h.mu.RUnlock()
	if !ok {
		return nil // 已经走了
	}
	// 尽力而为地通知插件优雅退出；它随后关流，读循环的 teardown 收尾
	if _, err := h.lifecycle(context.Background(), sess, pluginhostv1.Lifecycle_OP_SHUTDOWN); err != nil {
		h.Logf("下发 SHUTDOWN 失败（将强制回收）: %v", err)
	}
	h.teardown(sess, errors.New("宿主主动关闭"))
	return nil
}

func errorResponse(code, msg string, retryable bool) *pluginhostv1.InvokeResponse {
	return &pluginhostv1.InvokeResponse{
		Result: &contractv1.CallResult{
			Status: contractv1.CallResult_STATUS_ERROR,
			Error:  &contractv1.Error{Code: code, Message: msg, Retryable: retryable},
		},
	}
}

// failLaunch / readyLaunch：把接入结果回报给正在等待的 Launch。
func (h *Host) failLaunch(token string, err error) {
	if token == "" {
		return
	}
	h.mu.RLock()
	attempt, ok := h.launches[token]
	h.mu.RUnlock()
	if ok {
		select {
		case attempt.result <- launchResult{err: err}:
		default:
		}
	}
}

func (h *Host) readyLaunch(sess *session) {
	if sess.launchToken == "" {
		return
	}
	h.mu.RLock()
	attempt, ok := h.launches[sess.launchToken]
	h.mu.RUnlock()
	if ok {
		select {
		case attempt.result <- launchResult{sess: sess}:
		default:
		}
	}
}

func newSessionID() string { return "sess-" + randomHex(12) }
func newToken() string     { return "lt-" + randomHex(12) }
