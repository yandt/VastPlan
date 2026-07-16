package protocolbus

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	pluginhostv1 "cdsoft.com.cn/VastPlan/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/shared/go/protocol"
)

// Launch 拉起插件进程并等待它完成回连、握手、贡献注册与激活（§2.2）。
//
// 宿主注入连接端点 + magic cookie + 一次性 launch token；插件回连本宿主。
// 握手失败（magic/版本/engines）的原因经 launch token 回传，故此处能给出确切错误。
func (h *Host) Launch(ctx context.Context, binPath string) (*PluginProcess, error) {
	if h.addr == "" {
		return nil, errors.New("宿主尚未 Start，插件无处回连")
	}

	token := newToken()
	resultCh := make(chan launchResult, 1)
	h.mu.Lock()
	h.launches[token] = resultCh
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.launches, token)
		h.mu.Unlock()
	}()

	cmd := exec.Command(binPath)
	cmd.Env = append(cmd.Environ(),
		protocol.MagicEnvKey+"="+protocol.MagicCookie,
		protocol.HostAddrEnvKey+"="+h.addr,
		protocol.LaunchTokenEnvKey+"="+token,
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("拉起插件进程: %w", err)
	}
	h.Logf("插件进程已启动 pid=%d，等待回连 %s", cmd.Process.Pid, h.addr)

	// 进程提前退出（如 magic 不符自杀）时立刻脱身，不必等满超时
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	kill := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			kill()
			return nil, res.err
		}
		if !res.sess.bindProcess(cmd) {
			kill()
			return nil, fmt.Errorf("插件完成接入后立即失联: %w", res.sess.err())
		}
		return &PluginProcess{
			PluginID:  res.sess.pluginID,
			Version:   res.sess.pluginVersion,
			SessionID: res.sess.id,
			PID:       cmd.Process.Pid,
			session:   res.sess,
		}, nil

	case err := <-exited:
		// 进程没连上就退了；若握手已记录原因，resultCh 里会有更准确的错误
		select {
		case res := <-resultCh:
			if res.err != nil {
				return nil, res.err
			}
		default:
		}
		return nil, fmt.Errorf("插件进程未完成接入即退出: %v", err)

	case <-time.After(h.launchTimeout()):
		kill()
		return nil, fmt.Errorf("等待插件接入超时（%v）", h.launchTimeout())

	case <-ctx.Done():
		kill()
		return nil, ctx.Err()
	}
}

// Invoke 扩展点被触发时的**公开入口**，是完整的调用管道：
//
//	before 钩子（可一票否决）→ 权限判定 → 分发 → after 钩子（只观察）
//
// 权限按 select 语义走 permission.checker，零校验器 → fail-closed 拒绝（ADR-0021）。
// 钩子按 fanout 语义顺序执行，承载限流/配额/计量等横切关注点（皆为插件）。
// 未获放行/被否决均返回**应用层错误**（非传输层——工程规范 §4.2）。
func (h *Host) Invoke(ctx context.Context, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*pluginhostv1.InvokeResponse, error) {
	if err := h.enterCall(); err != nil {
		// Drain 是宿主的可用状态，不是 wire 故障；调用方应得到可重试的应用层结论，
		// 才能按正常路由切到候选实例，而不是把它误报为网络中断。
		return errorResponse(errorcode.PluginInactive, err.Error(), true), nil
	}
	defer h.leaveCall()

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
	resp, err := h.invoke(ctx, target, callCtx, payload)
	if err != nil {
		return nil, err // 传输层失败：无结论可供 after 钩子观察
	}

	// 4) after 钩子：计量/审计等只观察，不改变结论
	h.runAfterHooks(ctx, extpoint.PointInvoke, callCtx, target, resp.Result)
	return resp, nil
}

// invoke 内部分发：**不做权限判定**。
//
// 供两处使用：① 权限校验器自身的调用（否则自我递归）；② 事件扇出（内核发起的投递，
// 非用户调用）。业务路径一律走 Invoke。
func (h *Host) invoke(ctx context.Context, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*pluginhostv1.InvokeResponse, error) {

	c, ok := h.Registry.Lookup(target.ExtensionPoint, target.Capability)
	if !ok {
		return nil, fmt.Errorf("能力未注册：%s/%s", target.ExtensionPoint, target.Capability)
	}

	// 内核自身提供的能力：直接本地调用，不经流
	if c.PluginID == KernelPluginID {
		return h.callHostService(ctx, target, callCtx, payload)
	}

	h.mu.RLock()
	sess, ok := h.byPlugin[c.PluginID]
	h.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("能力 %s 的提供者 %s 当前未接入", target.Capability, c.PluginID)
	}
	return h.invokeOn(ctx, sess, target, callCtx, payload)
}

// invokeOn 在指定会话上发起一次调用并等待响应。
func (h *Host) invokeOn(ctx context.Context, sess *session, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*pluginhostv1.InvokeResponse, error) {

	reqID := sess.nextRequestID()
	ch := sess.await(reqID)
	defer sess.release(reqID)

	if err := sess.send(&pluginhostv1.FromHost{
		Msg: &pluginhostv1.FromHost_Invoke{
			Invoke: &pluginhostv1.InvokeRequest{
				RequestId: reqID, Target: target, Context: callCtx, Payload: payload,
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
	case <-time.After(h.callTimeout()):
		return nil, fmt.Errorf("调用超时（%v）", h.callTimeout())
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// serveHostCall 处理插件的回调：本地命中即内核服务，否则转给提供该能力的插件
// （即插件→插件也只经 capability 寻址，不得互相 import——见工程规范 §七）。
func (h *Host) serveHostCall(sess *session, req *pluginhostv1.InvokeRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), h.callTimeout())
	defer cancel()

	reply := func(resp *pluginhostv1.InvokeResponse) {
		resp.RequestId = req.RequestId
		if err := sess.send(&pluginhostv1.FromHost{
			Msg: &pluginhostv1.FromHost_HostCallResult{HostCallResult: resp},
		}); err != nil {
			h.Logf("回应插件 HostCall 失败: %v", err)
		}
	}

	h.Logf("插件 %s 回调宿主：%s/%s", sess.pluginID, req.Target.ExtensionPoint, req.Target.Capability)

	resp, err := h.Invoke(ctx, req.Target, req.Context, req.Payload)
	if err != nil {
		// 寻址/传输层失败 → 转为应用层错误回给插件，避免它把两类错误混为一谈
		reply(errorResponse(errorcode.HostCallFailed, err.Error(), false))
		return
	}
	reply(resp)
}

// callHostService 调用内核自身提供的能力。
func (h *Host) callHostService(ctx context.Context, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*pluginhostv1.InvokeResponse, error) {

	h.mu.RLock()
	fn, ok := h.services[target.Capability]
	h.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("内核能力 %s 无实现", target.Capability)
	}
	res, out, err := fn(ctx, callCtx, payload)
	if err != nil {
		return errorResponse(errorcode.KernelServiceError, err.Error(), true), nil
	}
	return &pluginhostv1.InvokeResponse{Result: res, Payload: out}, nil
}

// Close 优雅关闭插件：SHUTDOWN 指令 → 摘除贡献 → 回收进程。
func (h *Host) Close(p *PluginProcess) error {
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
	ch, ok := h.launches[token]
	h.mu.RUnlock()
	if ok {
		select {
		case ch <- launchResult{err: err}:
		default:
		}
	}
}

func (h *Host) readyLaunch(sess *session) {
	if sess.launchToken == "" {
		return
	}
	h.mu.RLock()
	ch, ok := h.launches[sess.launchToken]
	h.mu.RUnlock()
	if ok {
		select {
		case ch <- launchResult{sess: sess}:
		default:
		}
	}
}

func newSessionID() string { return "sess-" + randomHex(12) }
func newToken() string     { return "lt-" + randomHex(12) }
