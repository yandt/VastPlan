package protocolbus

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocollimit"
	"google.golang.org/protobuf/proto"
)

type processLogWriter struct {
	logf   func(string, ...any)
	prefix string
}

func (w processLogWriter) Write(raw []byte) (int, error) {
	const maxLine = 64 << 10
	line := strings.TrimSpace(string(raw))
	if len(line) > maxLine {
		line = line[:maxLine] + "…[truncated]"
	}
	if line != "" && w.logf != nil {
		w.logf("%s %s", w.prefix, line)
	}
	return len(raw), nil
}

// Launch 拉起插件进程并等待它完成回连、握手、贡献注册与激活（§2.2）。
//
// 宿主注入连接端点 + magic cookie + 一次性 launch token；插件回连本宿主。
// 握手失败（magic/版本/engines）的原因经 launch token 回传，故此处能给出确切错误。
func (h *Host) Launch(ctx context.Context, binPath string) (*PluginProcess, error) {
	return h.LaunchWithPolicy(ctx, binPath, LaunchPolicy{UnrestrictedContext: true})
}

// LaunchWithPolicy 启动插件，并把已验签清单中的身份、贡献和内核服务依赖绑定到
// 一次性 launch token。空 Policy 只用于本地演示/兼容夹具，但仍强制 token 认证。
func (h *Host) LaunchWithPolicy(ctx context.Context, binPath string, policy LaunchPolicy) (*PluginProcess, error) {
	return h.LaunchSpecWithPolicy(ctx, LaunchSpec{Command: binPath}, policy)
}

// LaunchSpecWithPolicy 通过运行驱动生成的无 shell 规格启动插件。
func (h *Host) LaunchSpecWithPolicy(ctx context.Context, spec LaunchSpec, policy LaunchPolicy) (*PluginProcess, error) {
	if h.addr == "" {
		return nil, errors.New("宿主尚未 Start，插件无处回连")
	}
	if strings.TrimSpace(spec.Command) == "" {
		return nil, errors.New("插件启动命令不能为空")
	}

	token := newToken()
	resultCh := make(chan launchResult, 1)
	h.mu.Lock()
	h.launches[token] = &launchAttempt{result: resultCh, policy: cloneLaunchPolicy(policy)}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.launches, token)
		h.mu.Unlock()
	}()

	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	environmentAllowlist := append([]string(nil), h.PluginEnvironmentAllowlist...)
	environmentAllowlist = append(environmentAllowlist, policy.EnvironmentAllowlist...)
	cmd.Env = append(pluginEnvironment(environmentAllowlist), spec.ExtraEnv...)
	cmd.Env = append(cmd.Env,
		protocol.MagicEnvKey+"="+protocol.MagicCookie,
		protocol.HostAddrEnvKey+"="+h.addr,
		protocol.LaunchTokenEnvKey+"="+token,
	)
	logID := policy.PluginID
	if logID == "" {
		logID = filepath.Base(spec.Command)
	}
	cmd.Stdout = processLogWriter{logf: h.Logf, prefix: "plugin=" + logID + " stream=stdout"}
	cmd.Stderr = processLogWriter{logf: h.Logf, prefix: "plugin=" + logID + " stream=stderr"}
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

func cloneLaunchPolicy(policy LaunchPolicy) LaunchPolicy {
	policy.Contributions = append([]pluginv1.RuntimeContribution(nil), policy.Contributions...)
	policy.KernelServices = append([]string(nil), policy.KernelServices...)
	policy.ContextAccess.Required = append([]string(nil), policy.ContextAccess.Required...)
	policy.ContextAccess.Optional = append([]string(nil), policy.ContextAccess.Optional...)
	policy.ContextAccess.Baggage = append([]string(nil), policy.ContextAccess.Baggage...)
	policy.ContextCeiling = append([]string(nil), policy.ContextCeiling...)
	policy.EnvironmentAllowlist = append([]string(nil), policy.EnvironmentAllowlist...)
	policy.RequiredFeatures = append([]string(nil), policy.RequiredFeatures...)
	return policy
}

func pluginEnvironment(allowlist []string) []string {
	allowed := append([]string(nil), allowlist...)
	// Windows 进程创建和部分系统库依赖这两个环境变量；它们不承载应用秘密。
	if runtime.GOOS == "windows" {
		allowed = append(allowed, "SystemRoot", "WINDIR")
	}
	sort.Strings(allowed)
	out := make([]string, 0, len(allowed))
	last := ""
	for _, key := range allowed {
		if key == "" || key == last {
			continue
		}
		last = key
		if value, ok := os.LookupEnv(key); ok {
			out = append(out, key+"="+value)
		}
	}
	return out
}

// Invoke 扩展点被触发时的**公开入口**，是完整的调用管道：
//
//	before 钩子（可一票否决）→ 权限判定 → 分发 → after 钩子（只观察）
//
// 权限按 select 语义走 permission.checker，零校验器 → fail-closed 拒绝（ADR-0021）。
// 钩子按 fanout 语义顺序执行，承载限流/配额/计量等横切关注点（皆为插件）。
// 未获放行/被否决均返回**应用层错误**（非传输层——工程规范 §4.2）。
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
		return nil, fmt.Errorf("能力未注册：%s/%s", target.ExtensionPoint, target.Capability)
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
	callCtx, ok := authenticatedPluginContext(sess, req.GetDelegationToken(), sess.pluginID)
	if !ok {
		h.replyHostCall(sess, req.RequestId, errorResponse(errorcode.PermissionDenied,
			"HostCall 缺少有效的宿主身份委托", false))
		return
	}
	if req.Target.ExtensionPoint == extpoint.KernelService && !kernelServiceAllowed(sess.policy, req.Target.Capability) {
		h.replyHostCall(sess, req.RequestId, errorResponse(errorcode.PermissionDenied,
			"插件未在签名清单中声明该内核服务", false))
		return
	}
	resp, err := h.Invoke(ctx, req.Target, callCtx, req.Payload)
	if err != nil {
		// 寻址/传输层失败 → 转为应用层错误回给插件，避免它把两类错误混为一谈
		h.replyHostCall(sess, req.RequestId, errorResponse(errorcode.HostCallFailed, err.Error(), false))
		return
	}
	h.replyHostCall(sess, req.RequestId, resp)
}

func kernelServiceAllowed(policy LaunchPolicy, capability string) bool {
	if capability == "" {
		return false
	}
	for _, allowed := range policy.KernelServices {
		if allowed == capability {
			return true
		}
	}
	return false
}

func authenticatedPluginContext(sess *session, token, pluginID string) (*contractv1.CallContext, bool) {
	bounded, ok := sess.delegatedContext(token)
	if !ok {
		return nil, false
	}
	bounded.Caller = &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginID}
	return bounded, true
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
