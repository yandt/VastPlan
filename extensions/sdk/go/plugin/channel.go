package plugin

import (
	"context"
	"fmt"
	"os"
	"time"

	"google.golang.org/protobuf/proto"

	"cdsoft.com.cn/VastPlan/contracts/runtime/go/errorcode"
	pluginhostv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/protocol"
)

func (p *Plugin) declaration() *pluginhostv1.Declaration {
	p.contribMu.RLock()
	defer p.contribMu.RUnlock()
	out := &pluginhostv1.Declaration{}
	for _, c := range p.contribs {
		out.Contributions = append(out.Contributions, &pluginhostv1.Contribution{
			ExtensionPoint: c.ExtensionPoint,
			Id:             c.ID,
			Priority:       c.Priority,
			DescriptorJson: c.Descriptor,
		})
	}
	return out
}

// send 向宿主发一条消息（串行化：gRPC 流不允许并发 Send）。
func (p *Plugin) send(msg *pluginhostv1.FromPlugin) error {
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	return p.stream.Send(msg)
}

// readLoop 收宿主消息并分发；宿主断开或下发 SHUTDOWN 时返回。
func (p *Plugin) readLoop() error {
	lifecycleQueue := make(chan *pluginhostv1.Lifecycle, p.Limits.Normalize().MaxPendingRequests)
	lifecycleDone := make(chan struct{})
	defer close(lifecycleDone)
	go p.lifecycleLoop(lifecycleQueue, lifecycleDone)
	for {
		msg, err := p.stream.Recv()
		if err != nil {
			if p.shuttingDown.Load() {
				return nil
			}
			return err // 宿主断开 → 插件退出（内核内协议，宿主没了插件无意义）
		}

		switch m := msg.Msg.(type) {
		case *pluginhostv1.FromHost_Registered:
			for id, why := range m.Registered.Rejected {
				fmt.Fprintf(os.Stderr, "贡献 %s 被宿主拒绝: %s\n", id, why)
			}
		case *pluginhostv1.FromHost_Invoke:
			if rejected := p.beginInvoke(m.Invoke); rejected != nil {
				p.sendInvokeResponse(m.Invoke, rejected)
				continue
			}
			go p.handleInvoke(m.Invoke) // 已占固定并发槽，不会形成无界 goroutine
		case *pluginhostv1.FromHost_Lifecycle:
			if m.Lifecycle == nil {
				fmt.Fprintln(os.Stderr, "忽略空生命周期消息")
				continue
			}
			select {
			case lifecycleQueue <- m.Lifecycle:
			default:
				message := "生命周期 pending 队列已满"
				_ = p.send(&pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_LifecycleAck{
					LifecycleAck: &pluginhostv1.LifecycleAck{RequestId: m.Lifecycle.RequestId, Ready: false, Message: &message},
				}})
			}
		case *pluginhostv1.FromHost_Ping:
			_ = p.send(&pluginhostv1.FromPlugin{
				Msg: &pluginhostv1.FromPlugin_Pong{Pong: &pluginhostv1.Pong{RequestId: m.Ping.RequestId}},
			})
		case *pluginhostv1.FromHost_HostCallResult:
			p.deliver(m.HostCallResult.RequestId, msg)
		case *pluginhostv1.FromHost_ContributionUpdateAck:
			p.deliver(m.ContributionUpdateAck.RequestId, msg)
		case *pluginhostv1.FromHost_Cancel:
			if p.hasFeature(protocol.FeatureCancellation) && m.Cancel != nil {
				p.cancelInvoke(m.Cancel.RequestId)
			}
		case *pluginhostv1.FromHost_Event:
			// 事件订阅待 event.sink 扩展点落地后接入
		}
	}
}

// lifecycleLoop 串行执行生命周期操作但与 Recv 循环分离。这样迁移或 drain 等待期间
// 仍能接收 Ping 与 HostCallResult，同时生命周期之间继续保持严格顺序。
func (p *Plugin) lifecycleLoop(queue <-chan *pluginhostv1.Lifecycle, done <-chan struct{}) {
	for {
		select {
		case lc := <-queue:
			p.handleLifecycle(lc)
		case <-done:
			return
		}
	}
}

func (p *Plugin) handleInvoke(req *pluginhostv1.InvokeRequest) {
	defer p.endInvoke(req.GetRequestId())
	resp := p.dispatchInvoke(req)
	p.sendInvokeResponse(req, resp)
}

func (p *Plugin) sendInvokeResponse(req *pluginhostv1.InvokeRequest, resp *pluginhostv1.InvokeResponse) {
	if req == nil {
		return
	}
	resp.RequestId = req.RequestId
	if err := p.send(&pluginhostv1.FromPlugin{
		Msg: &pluginhostv1.FromPlugin_InvokeResult{InvokeResult: resp},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "回送调用结果失败: %v\n", err)
	}
}

func (p *Plugin) dispatchInvoke(req *pluginhostv1.InvokeRequest) *pluginhostv1.InvokeResponse {
	limits := p.Limits.Normalize()
	if req == nil || req.Target == nil {
		return errResult(errorcode.WireInvalidRequest, "调用请求不完整", false)
	}
	if !limits.PayloadAllowed(req.Payload) {
		return errResult(errorcode.PayloadTooLarge,
			fmt.Sprintf("payload 为 %d bytes，超过上限 %d bytes", len(req.Payload), limits.MaxPayloadBytes), false)
	}
	if !limits.MetadataAllowed(proto.Size(req.Context)) {
		return errResult(errorcode.MetadataTooLarge,
			fmt.Sprintf("CallContext 为 %d bytes，超过 metadata 上限 %d bytes", proto.Size(req.Context), limits.MaxMetadataBytes), false)
	}
	op := ""
	if req.Target.Operation != nil {
		op = *req.Target.Operation
	}
	p.contribMu.RLock()
	h, ok := p.routes[routeKey(req.Target.ExtensionPoint, req.Target.Capability, op)]
	if !ok {
		h, ok = p.routes[routeKey(req.Target.ExtensionPoint, req.Target.Capability, "")] // 默认处理器
	}
	p.contribMu.RUnlock()
	if !ok {
		return errResult(errorcode.CapabilityNotFound,
			fmt.Sprintf("未实现 %s/%s 的操作 %q", req.Target.ExtensionPoint, req.Target.Capability, op), false)
	}

	ctx := context.Background()
	p.invokeMu.Lock()
	if registered := p.invokeContexts[req.RequestId]; registered != nil {
		ctx = registered
	}
	p.invokeMu.Unlock()
	var cancel context.CancelFunc
	if req.Context != nil && req.Context.DeadlineUnixMs != nil {
		ctx, cancel = context.WithDeadline(ctx, time.UnixMilli(*req.Context.DeadlineUnixMs))
	} else {
		ctx, cancel = context.WithTimeout(ctx, limits.DefaultDeadline)
	}
	defer cancel()
	if req.Context != nil {
		ctx = context.WithValue(ctx, invocationCallPathKey{}, append([]string(nil), req.Context.CallPath...))
	}
	if token := req.GetDelegationToken(); token != "" {
		ctx = context.WithValue(ctx, invocationDelegationKey{}, token)
	}

	res, payload, err := h(ctx, p, req.Context, req.Payload)
	if err != nil {
		// 应用层错误进 CallResult，不与传输层错误混淆（工程规范 §4.2）
		return errResult(errorcode.PluginHandlerError, err.Error(), true)
	}
	if !limits.PayloadAllowed(payload) {
		return errResult(errorcode.PayloadTooLarge,
			fmt.Sprintf("响应 payload 为 %d bytes，超过上限 %d bytes", len(payload), limits.MaxPayloadBytes), false)
	}
	return &pluginhostv1.InvokeResponse{Result: res, Payload: payload}
}

func (p *Plugin) beginInvoke(req *pluginhostv1.InvokeRequest) *pluginhostv1.InvokeResponse {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	if !p.active {
		return errResult(errorcode.PluginInactive, "插件未激活", false)
	}
	if p.inflightN >= p.Limits.Normalize().MaxConcurrentCalls {
		return errResult(errorcode.ConcurrencyLimited, "插件处理器并发达到上限", true)
	}
	p.inflightN++
	p.inflight.Add(1)
	if req != nil && req.RequestId != "" {
		invokeCtx, cancel := context.WithCancel(context.Background())
		p.invokeMu.Lock()
		p.invokeContexts[req.RequestId] = invokeCtx
		p.invokeCancels[req.RequestId] = cancel
		p.invokeMu.Unlock()
	}
	return nil
}

func (p *Plugin) endInvoke(requestID ...string) {
	if len(requestID) > 0 && requestID[0] != "" {
		p.invokeMu.Lock()
		cancel := p.invokeCancels[requestID[0]]
		delete(p.invokeContexts, requestID[0])
		delete(p.invokeCancels, requestID[0])
		p.invokeMu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	p.lifecycleMu.Lock()
	if p.inflightN > 0 {
		p.inflightN--
	}
	p.lifecycleMu.Unlock()
	p.inflight.Done()
}

// handleLifecycle 在独立的串行 worker 中处理生命周期指令。
