// Package plugin 是第一方插件开发 SDK 的 Go 实现（backend 面）。
//
// 插件只需：声明贡献 + 实现处理器，SDK 负责协议细节（回连、握手、声明、
// 双向流收发、生命周期、心跳）。协议规格见 docs/dev/architecture/插件契约与协议.md 第二章。
package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/shared/go/protocol"
)

// hostCallTimeout 插件回调宿主的等待上限——宿主无响应时处理器必须能脱身。
const hostCallTimeout = 30 * time.Second

// Host 是插件回调宿主的入口：取内核服务、或经 capability 寻址调别的能力（§2.4）。
// 插件**不得**直接 import 别的插件，只能经它按能力名寻址（工程规范 §七）。
type Host interface {
	Call(ctx context.Context, target *contractv1.CallTarget,
		callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error)
}

// Handler 处理一次扩展点调用：收 CallContext + payload，回 CallResult + payload。
// host 参数使处理器可回调宿主（不需要它时忽略即可）。
type Handler func(ctx context.Context, host Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error)

// Contribution 插件对某扩展点的一条贡献。
type Contribution struct {
	ExtensionPoint string // 如 tool.package
	ID             string // 稳定逻辑名（= 清单 id = CallTarget.capability）
	Priority       int32
	Descriptor     []byte // 该扩展点的贡献契约（JSON，见第四章）
	// Handlers 按 operation 分发；key "" 为默认处理器
	Handlers map[string]Handler
}

// Plugin 一个插件进程。
type Plugin struct {
	ID      string
	Version string // SemVer，单一真源 = vastplan.plugin.json#version（ADR-0017 §1）
	// Engines 清单 engines：{内核规范ID: SemVer 范围}。宿主据此校验自身版本（ADR-0017 §4）。
	Engines map[string]string

	contribs []Contribution
	routes   map[string]Handler // (extensionPoint, id, operation) → Handler

	stream pluginhostv1.PluginHost_ChannelClient
	sendMu sync.Mutex

	// lifecycleMu 把“是否接受新调用”与 inflight.Add 做成一个门闩。
	// DRAIN 关门后再 Wait，保证不会发生 Wait 与后续 Add 竞态。
	lifecycleMu sync.Mutex
	active      bool
	inflight    sync.WaitGroup
	sessionID   string

	pendingMu sync.Mutex
	pending   map[string]chan *pluginhostv1.FromHost
	seq       atomic.Uint64
}

func New(id, version string, engines map[string]string) *Plugin {
	if engines == nil {
		engines = map[string]string{}
	}
	return &Plugin{
		ID: id, Version: version, Engines: engines,
		routes:  map[string]Handler{},
		pending: map[string]chan *pluginhostv1.FromHost{},
	}
}

// Contribute 登记一条贡献（在 Serve 前调用）。
func (p *Plugin) Contribute(c Contribution) {
	p.contribs = append(p.contribs, c)
	for op, h := range c.Handlers {
		p.routes[routeKey(c.ExtensionPoint, c.ID, op)] = h
	}
}

func routeKey(ep, id, op string) string { return ep + "|" + id + "|" + op }

// Serve 回连宿主、完成握手与贡献声明，然后进入运行态直到宿主断开或下发 SHUTDOWN。
func (p *Plugin) Serve() error {
	// magic 校验：宿主经 env 注入，防止被当普通程序误启
	if os.Getenv(protocol.MagicEnvKey) != protocol.MagicCookie {
		return errors.New("magic cookie 不匹配：本程序是 VastPlan 插件，须由宿主拉起")
	}
	hostAddr := os.Getenv(protocol.HostAddrEnvKey)
	if hostAddr == "" {
		return fmt.Errorf("未注入宿主地址（%s）", protocol.HostAddrEnvKey)
	}

	conn, err := grpc.NewClient(hostAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("回连宿主失败: %w", err)
	}
	defer func() { _ = conn.Close() }()
	client := pluginhostv1.NewPluginHostClient(conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1) 握手：自报身份 + 版本集 + engines；宿主校验后签发会话票据
	ack, err := client.Handshake(ctx, &pluginhostv1.Hello{
		ProtoVersions: protocol.SupportedVersions,
		Magic:         protocol.MagicCookie,
		PluginId:      p.ID,
		PluginVersion: p.Version,
		Engines:       p.Engines,
		LaunchToken:   os.Getenv(protocol.LaunchTokenEnvKey),
	})
	if err != nil {
		return fmt.Errorf("握手被拒: %w", err) // 宿主已说明原因（magic/版本/engines）
	}
	if !protocol.Supports(ack.NegotiatedProto) {
		return fmt.Errorf("宿主回了本插件不支持的协议版本 %d", ack.NegotiatedProto)
	}
	p.sessionID = ack.SessionId

	// 2) 建立双向流：会话票据经 metadata 携带
	streamCtx := metadata.AppendToOutgoingContext(ctx, protocol.SessionMetadataKey, p.sessionID)
	stream, err := client.Channel(streamCtx)
	if err != nil {
		return fmt.Errorf("建立 Channel 失败: %w", err)
	}
	p.stream = stream

	// 3) 声明贡献（流上首条消息）
	if err := p.send(&pluginhostv1.FromPlugin{
		Msg: &pluginhostv1.FromPlugin_Declare{Declare: p.declaration()},
	}); err != nil {
		return fmt.Errorf("发送贡献声明失败: %w", err)
	}

	// 4) 运行态读循环
	return p.readLoop()
}

func (p *Plugin) declaration() *pluginhostv1.Declaration {
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
	for {
		msg, err := p.stream.Recv()
		if err != nil {
			return err // 宿主断开 → 插件退出（内核内协议，宿主没了插件无意义）
		}

		switch m := msg.Msg.(type) {
		case *pluginhostv1.FromHost_Registered:
			for id, why := range m.Registered.Rejected {
				fmt.Fprintf(os.Stderr, "贡献 %s 被宿主拒绝: %s\n", id, why)
			}
		case *pluginhostv1.FromHost_Invoke:
			go p.handleInvoke(m.Invoke) // 不阻塞读循环：慢处理器不得拖住心跳
		case *pluginhostv1.FromHost_Lifecycle:
			if shutdown := p.handleLifecycle(m.Lifecycle); shutdown {
				return nil
			}
		case *pluginhostv1.FromHost_Ping:
			_ = p.send(&pluginhostv1.FromPlugin{
				Msg: &pluginhostv1.FromPlugin_Pong{Pong: &pluginhostv1.Pong{RequestId: m.Ping.RequestId}},
			})
		case *pluginhostv1.FromHost_HostCallResult:
			p.deliver(m.HostCallResult.RequestId, msg)
		case *pluginhostv1.FromHost_Event:
			// 事件订阅待 event.sink 扩展点落地后接入
		}
	}
}

func (p *Plugin) handleInvoke(req *pluginhostv1.InvokeRequest) {
	resp := p.dispatchInvoke(req)
	resp.RequestId = req.RequestId
	if err := p.send(&pluginhostv1.FromPlugin{
		Msg: &pluginhostv1.FromPlugin_InvokeResult{InvokeResult: resp},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "回送调用结果失败: %v\n", err)
	}
}

func (p *Plugin) dispatchInvoke(req *pluginhostv1.InvokeRequest) *pluginhostv1.InvokeResponse {
	if !p.beginInvoke() {
		return errResult("plugin.inactive", "插件未激活", false)
	}
	defer p.inflight.Done()
	op := ""
	if req.Target.Operation != nil {
		op = *req.Target.Operation
	}
	h, ok := p.routes[routeKey(req.Target.ExtensionPoint, req.Target.Capability, op)]
	if !ok {
		h, ok = p.routes[routeKey(req.Target.ExtensionPoint, req.Target.Capability, "")] // 默认处理器
	}
	if !ok {
		return errResult("capability.not_found",
			fmt.Sprintf("未实现 %s/%s 的操作 %q", req.Target.ExtensionPoint, req.Target.Capability, op), false)
	}

	ctx := context.Background()
	if req.Context != nil && req.Context.DeadlineUnixMs != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, time.UnixMilli(*req.Context.DeadlineUnixMs))
		defer cancel()
	}

	res, payload, err := h(ctx, p, req.Context, req.Payload)
	if err != nil {
		// 应用层错误进 CallResult，不与传输层错误混淆（工程规范 §4.2）
		return errResult("plugin.handler_error", err.Error(), true)
	}
	return &pluginhostv1.InvokeResponse{Result: res, Payload: payload}
}

func (p *Plugin) beginInvoke() bool {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	if !p.active {
		return false
	}
	p.inflight.Add(1)
	return true
}

// handleLifecycle 处理生命周期指令；返回 true 表示应退出。
func (p *Plugin) handleLifecycle(lc *pluginhostv1.Lifecycle) bool {
	ack := &pluginhostv1.LifecycleAck{RequestId: lc.RequestId, Ready: true}
	shutdown := false

	switch lc.Op {
	case pluginhostv1.Lifecycle_OP_ACTIVATE:
		p.lifecycleMu.Lock()
		p.active = true
		p.lifecycleMu.Unlock()
	case pluginhostv1.Lifecycle_OP_DEACTIVATE:
		p.lifecycleMu.Lock()
		p.active = false
		p.lifecycleMu.Unlock()
	case pluginhostv1.Lifecycle_OP_DRAIN:
		p.lifecycleMu.Lock()
		p.active = false
		p.lifecycleMu.Unlock()
		p.inflight.Wait()
	case pluginhostv1.Lifecycle_OP_SHUTDOWN:
		p.lifecycleMu.Lock()
		p.active = false
		p.lifecycleMu.Unlock()
		p.inflight.Wait()
		shutdown = true
	default:
		msg := "未知生命周期指令"
		ack.Ready, ack.Message = false, &msg
	}

	_ = p.send(&pluginhostv1.FromPlugin{
		Msg: &pluginhostv1.FromPlugin_LifecycleAck{LifecycleAck: ack},
	})
	return shutdown
}

// Call 实现 Host：插件回调宿主（内核服务，或经 capability 寻址调别的插件）。
func (p *Plugin) Call(ctx context.Context, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {

	reqID := fmt.Sprintf("hc-%d", p.seq.Add(1))
	ch := make(chan *pluginhostv1.FromHost, 1)

	p.pendingMu.Lock()
	p.pending[reqID] = ch
	p.pendingMu.Unlock()
	defer func() {
		p.pendingMu.Lock()
		delete(p.pending, reqID)
		p.pendingMu.Unlock()
	}()

	if err := p.send(&pluginhostv1.FromPlugin{
		Msg: &pluginhostv1.FromPlugin_HostCall{
			HostCall: &pluginhostv1.InvokeRequest{
				RequestId: reqID, Target: target, Context: callCtx, Payload: payload,
			},
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("回调宿主失败: %w", err)
	}

	select {
	case msg := <-ch:
		r := msg.GetHostCallResult()
		return r.Result, r.Payload, nil
	case <-time.After(hostCallTimeout):
		return nil, nil, fmt.Errorf("回调宿主超时（%v）", hostCallTimeout)
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (p *Plugin) deliver(reqID string, msg *pluginhostv1.FromHost) {
	p.pendingMu.Lock()
	ch, ok := p.pending[reqID]
	p.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

func errResult(code, msg string, retryable bool) *pluginhostv1.InvokeResponse {
	return &pluginhostv1.InvokeResponse{
		Result: &contractv1.CallResult{
			Status: contractv1.CallResult_STATUS_ERROR,
			Error:  &contractv1.Error{Code: code, Message: msg, Retryable: retryable},
		},
	}
}

// OK 构造一个成功的 CallResult（便利函数）。
func OK(durationMs int64) *contractv1.CallResult {
	return &contractv1.CallResult{
		Status: contractv1.CallResult_STATUS_OK,
		Usage:  &contractv1.Usage{DurationMs: durationMs},
	}
}
