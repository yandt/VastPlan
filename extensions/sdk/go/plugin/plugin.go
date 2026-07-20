// Package plugin 是第一方插件开发 SDK 的 Go 实现（backend 面）。
//
// 插件只需：声明贡献 + 实现处理器，SDK 负责协议细节（回连、握手、声明、
// 双向流收发、生命周期、心跳）。协议规格见 docs/dev/architecture/插件契约与协议.md 第二章。
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocollimit"
)

// DecodeStartupConfiguration decodes the caller-isolated, non-sensitive
// configuration snapshot injected into an independent plugin process by the
// trusted runtime. Managed Runtime Hosts decode their per-unit environment map;
// dynamic plugins use kernel.config.get. Unknown fields fail closed.
func DecodeStartupConfiguration(output any) error {
	return decodeStartupConfiguration(os.Getenv(protocol.PluginConfigEnvKey), output)
}

func decodeStartupConfiguration(raw string, output any) error {
	if output == nil {
		return errors.New("启动配置输出不能为空")
	}
	if raw == "" {
		raw = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("解析插件启动配置: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("插件启动配置必须只包含一个 JSON 值")
	}
	return nil
}

// Host 是插件回调宿主的入口：取内核服务、或经 capability 寻址调别的能力（§2.4）。
// 插件**不得**直接 import 别的插件，只能经它按能力名寻址（工程规范 §七）。
type Host interface {
	Call(ctx context.Context, target *contractv1.CallTarget,
		callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error)
}

// Handler 处理一次扩展点调用：收 CallContext + payload，回 CallResult + payload。
// host 参数使处理器可回调宿主（不需要它时忽略即可）。
type Handler func(ctx context.Context, host Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error)

// ContextViews returns defensive, semantic views over the host-projected
// context. It does not expose fields removed by audience projection.
func ContextViews(callCtx *contractv1.CallContext) callcontext.Views {
	return callcontext.ReadOnlyViews(callCtx)
}

// invocationCallPathKey 只在一次处理器调用的 context 内携带宿主已验证的调用路径。
// Plugin.Call 会用它覆盖处理器可能传入的旧副本，保证链路继续向下游传播。
type invocationCallPathKey struct{}

// MigrationPhase 是插件私有状态 copy-on-write 事务的阶段。COMMIT 只提交候选视图，
// 在宿主切换路由所有权前仍必须允许 ROLLBACK；插件不得修改旧实例正在读取的视图。
type MigrationPhase string

const (
	MigrationPrepare  MigrationPhase = "prepare"
	MigrationCommit   MigrationPhase = "commit"
	MigrationRollback MigrationPhase = "rollback"
)

type StateIdentity = pluginv1.StateIdentity

type MigrationRequest = pluginv1.MigrationRequest

// MigrationHandler 必须按 TransactionID 幂等。重复 PREPARE/COMMIT/ROLLBACK 不得
// 产生额外副作用；返回错误会使候选启动失败并保留当前版本。
type MigrationHandler func(context.Context, MigrationPhase, MigrationRequest) error

// LifecycleHandler is the low-level hook used by trusted Runtime Providers to
// adapt an existing module ABI. Ordinary plugins should prefer OnMigration.
type LifecycleHandler func(context.Context, *pluginhostv1.Lifecycle) error

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
	// Limits 与宿主使用同一资源契约；零值字段自动采用统一安全默认。
	Limits protocollimit.Limits

	contribs  []Contribution
	routes    map[string]Handler // (extensionPoint, id, operation) → Handler
	contribMu sync.RWMutex

	stream pluginhostv1.PluginHost_ChannelClient
	sendMu sync.Mutex

	// lifecycleMu 把“是否接受新调用”与 inflight.Add 做成一个门闩。
	// DRAIN 关门后再 Wait，保证不会发生 Wait 与后续 Add 竞态。
	lifecycleMu sync.Mutex
	active      bool
	inflightN   int
	inflight    sync.WaitGroup
	sessionID   string
	migration   MigrationHandler
	lifecycle   LifecycleHandler

	pendingMu      sync.Mutex
	pending        map[string]chan *pluginhostv1.FromHost
	invokeMu       sync.Mutex
	invokeContexts map[string]context.Context
	invokeCancels  map[string]context.CancelFunc
	features       map[string]bool
	seq            atomic.Uint64
	// shuttingDown 让宿主在收到异步 SHUTDOWN Ack 后关流时被识别为正常退出。
	shuttingDown  atomic.Bool
	serveMu       sync.Mutex
	serveCancel   context.CancelFunc
	serveConn     *grpc.ClientConn
	stopRequested atomic.Bool
}

// OnMigration 登记插件私有状态迁移处理器。只有清单 state.backend 声明了
// lifecycle.v1 的插件才应设置；未设置却收到迁移指令时 SDK 会 fail-closed。
func (p *Plugin) OnMigration(handler MigrationHandler) {
	p.migration = handler
}

// OnLifecycle registers a trusted adapter hook for all lifecycle operations.
// Returning an error rejects the operation before the SDK changes local state.
func (p *Plugin) OnLifecycle(handler LifecycleHandler) { p.lifecycle = handler }

func New(id, version string, engines map[string]string) *Plugin {
	if engines == nil {
		engines = map[string]string{}
	}
	return &Plugin{
		ID: id, Version: version, Engines: engines,
		Limits: protocollimit.Default(), routes: map[string]Handler{},
		pending:        map[string]chan *pluginhostv1.FromHost{},
		invokeContexts: map[string]context.Context{}, invokeCancels: map[string]context.CancelFunc{},
		features: map[string]bool{},
	}
}

// Contribute 登记一条贡献（在 Serve 前调用）。
func (p *Plugin) Contribute(c Contribution) {
	p.contribMu.Lock()
	defer p.contribMu.Unlock()
	p.contribs = append(p.contribs, c)
	for op, h := range c.Handlers {
		p.routes[routeKey(c.ExtensionPoint, c.ID, op)] = h
	}
}

func routeKey(ep, id, op string) string { return ep + "|" + id + "|" + op }

// Serve 回连宿主、完成握手与贡献声明，然后进入运行态直到宿主断开或下发 SHUTDOWN。
func (p *Plugin) Serve() error {
	return p.ServeWithEnvironment(nil)
}

// ServeWithEnvironment avoids process-global environment mutation when a
// shared Runtime Host serves several plugin sessions. A nil map preserves the
// independent-process behavior of Serve.
func (p *Plugin) ServeWithEnvironment(environment map[string]string) error {
	lookup := os.Getenv
	if environment != nil {
		frozen := make(map[string]string, len(environment))
		for key, value := range environment {
			frozen[key] = value
		}
		lookup = func(key string) string { return frozen[key] }
	}
	// magic 校验：宿主经 env 注入，防止被当普通程序误启
	if lookup(protocol.MagicEnvKey) != protocol.MagicCookie {
		return errors.New("magic cookie 不匹配：本程序是 VastPlan 插件，须由宿主拉起")
	}
	hostAddr := lookup(protocol.HostAddrEnvKey)
	if hostAddr == "" {
		return fmt.Errorf("未注入宿主地址（%s）", protocol.HostAddrEnvKey)
	}

	limits := p.Limits.Normalize()
	conn, err := grpc.NewClient(hostAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithMaxHeaderListSize(limits.MaxMetadataBytes),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(limits.MaxMessageBytes()),
			grpc.MaxCallSendMsgSize(limits.MaxMessageBytes()),
		),
	)
	if err != nil {
		return fmt.Errorf("回连宿主失败: %w", err)
	}
	defer func() { _ = conn.Close() }()
	client := pluginhostv1.NewPluginHostClient(conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.serveMu.Lock()
	p.serveCancel, p.serveConn = cancel, conn
	stopRequested := p.stopRequested.Load()
	p.serveMu.Unlock()
	if stopRequested {
		cancel()
		_ = conn.Close()
		return errors.New("插件会话已请求停止")
	}
	defer func() {
		p.serveMu.Lock()
		p.serveCancel, p.serveConn = nil, nil
		p.serveMu.Unlock()
	}()

	// 1) 握手：自报身份 + 版本集 + engines；宿主校验后签发会话票据
	ack, err := client.Handshake(ctx, &pluginhostv1.Hello{
		ProtoVersions: protocol.SupportedVersions,
		Magic:         protocol.MagicCookie,
		PluginId:      p.ID,
		PluginVersion: p.Version,
		Engines:       p.Engines,
		LaunchToken:   lookup(protocol.LaunchTokenEnvKey),
		Features:      protocol.SupportedFeatures,
	})
	if err != nil {
		return fmt.Errorf("握手被拒: %w", err) // 宿主已说明原因（magic/版本/engines）
	}
	if !protocol.Supports(ack.NegotiatedProto) {
		return fmt.Errorf("宿主回了本插件不支持的协议版本 %d", ack.NegotiatedProto)
	}
	p.sessionID = ack.SessionId
	for _, feature := range ack.NegotiatedFeatures {
		p.features[feature] = true
	}

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

// Shutdown stops one logical plugin session without terminating a shared
// Runtime Host process. It is idempotent and also aborts a pending handshake.
func (p *Plugin) Shutdown() {
	p.stopRequested.Store(true)
	p.serveMu.Lock()
	cancel, conn := p.serveCancel, p.serveConn
	p.serveMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
}

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
func (p *Plugin) handleLifecycle(lc *pluginhostv1.Lifecycle) {
	ack := &pluginhostv1.LifecycleAck{RequestId: lc.RequestId, Ready: true}
	if p.lifecycle != nil {
		lifecycleCtx, cancel := context.WithTimeout(context.Background(), p.Limits.Normalize().DefaultDeadline)
		err := p.lifecycle(lifecycleCtx, lc)
		cancel()
		if err != nil {
			ack.Ready = false
			message := err.Error()
			ack.Message = &message
			_ = p.send(&pluginhostv1.FromPlugin{
				Msg: &pluginhostv1.FromPlugin_LifecycleAck{LifecycleAck: ack},
			})
			return
		}
	}

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
		p.shuttingDown.Store(true)
	case pluginhostv1.Lifecycle_OP_MIGRATION_PREPARE,
		pluginhostv1.Lifecycle_OP_MIGRATION_COMMIT,
		pluginhostv1.Lifecycle_OP_MIGRATION_ROLLBACK:
		if p.lifecycle != nil {
			break // trusted adapter already handled the full wire lifecycle
		}
		phase, err := migrationPhase(lc.Op)
		if err != nil {
			ack.Ready = false
			msg := err.Error()
			ack.Message = &msg
			break
		}
		if p.migration == nil {
			ack.Ready = false
			msg := "插件未实现清单声明的状态迁移处理器"
			ack.Message = &msg
			break
		}
		request := MigrationRequest{
			TransactionID: lc.TransactionId,
			From:          StateIdentity{Format: lc.FromStateFormat, FormatVersion: lc.FromStateVersion},
			To:            StateIdentity{Format: lc.ToStateFormat, FormatVersion: lc.ToStateVersion},
		}
		if request.TransactionID == "" || request.From.Format == "" || request.From.FormatVersion <= 0 ||
			request.To.Format == "" || request.To.FormatVersion <= 0 {
			ack.Ready = false
			msg := "状态迁移请求字段不完整"
			ack.Message = &msg
			break
		}
		migrationCtx, cancel := context.WithTimeout(context.Background(), p.Limits.Normalize().DefaultDeadline)
		err = p.migration(migrationCtx, phase, request)
		cancel()
		if err != nil {
			ack.Ready = false
			msg := err.Error()
			ack.Message = &msg
		}
	default:
		msg := "未知生命周期指令"
		ack.Ready, ack.Message = false, &msg
	}

	_ = p.send(&pluginhostv1.FromPlugin{
		Msg: &pluginhostv1.FromPlugin_LifecycleAck{LifecycleAck: ack},
	})
}

func migrationPhase(op pluginhostv1.Lifecycle_Op) (MigrationPhase, error) {
	switch op {
	case pluginhostv1.Lifecycle_OP_MIGRATION_PREPARE:
		return MigrationPrepare, nil
	case pluginhostv1.Lifecycle_OP_MIGRATION_COMMIT:
		return MigrationCommit, nil
	case pluginhostv1.Lifecycle_OP_MIGRATION_ROLLBACK:
		return MigrationRollback, nil
	default:
		return "", fmt.Errorf("生命周期指令 %s 不是迁移阶段", op)
	}
}

// Call 实现 Host：插件回调宿主（内核服务，或经 capability 寻址调别的插件）。
func (p *Plugin) Call(ctx context.Context, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	limits := p.Limits.Normalize()
	if target == nil || target.Capability == "" {
		return nil, nil, errors.New("回调宿主的目标 capability 不能为空")
	}
	if !limits.PayloadAllowed(payload) {
		return nil, nil, fmt.Errorf("回调宿主 payload 为 %d bytes，超过上限 %d bytes", len(payload), limits.MaxPayloadBytes)
	}
	if !limits.MetadataAllowed(proto.Size(callCtx)) {
		return nil, nil, fmt.Errorf("回调宿主 CallContext 为 %d bytes，超过 metadata 上限 %d bytes", proto.Size(callCtx), limits.MaxMetadataBytes)
	}
	ctx, callCtx, cancel := pluginCallContext(ctx, callCtx, limits.DefaultDeadline)
	defer cancel()

	reqID := fmt.Sprintf("hc-%d", p.seq.Add(1))
	ch := make(chan *pluginhostv1.FromHost, 1)

	p.pendingMu.Lock()
	if len(p.pending) >= limits.MaxPendingRequests {
		p.pendingMu.Unlock()
		return nil, nil, errors.New("回调宿主 pending 请求队列已满")
	}
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
				DelegationToken: delegationTokenFromContext(ctx),
			},
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("回调宿主失败: %w", err)
	}

	select {
	case msg := <-ch:
		r := msg.GetHostCallResult()
		if r == nil {
			return nil, nil, errors.New("宿主回调响应为空")
		}
		if !limits.PayloadAllowed(r.Payload) {
			return nil, nil, fmt.Errorf("宿主回调响应 payload 为 %d bytes，超过上限 %d bytes", len(r.Payload), limits.MaxPayloadBytes)
		}
		return r.Result, r.Payload, nil
	case <-ctx.Done():
		if p.hasFeature(protocol.FeatureCancellation) {
			_ = p.send(&pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_Cancel{
				Cancel: &pluginhostv1.Cancel{RequestId: reqID},
			}})
		}
		return nil, nil, ctx.Err()
	}
}

type invocationDelegationKey struct{}

func delegationTokenFromContext(ctx context.Context) *string {
	if ctx == nil {
		return nil
	}
	token, _ := ctx.Value(invocationDelegationKey{}).(string)
	if token == "" {
		return nil
	}
	return &token
}

func (p *Plugin) hasFeature(feature string) bool { return p.features[feature] }

func (p *Plugin) cancelInvoke(requestID string) {
	p.invokeMu.Lock()
	cancel := p.invokeCancels[requestID]
	p.invokeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// PublishEvent 把插件事件发布到宿主事件总线。宿主会覆盖 source 并清除
// principal_ref；调用方不能借事件字段伪造权威身份。
func (p *Plugin) PublishEvent(event *contractv1.CallEvent) error {
	if !p.hasFeature(protocol.FeatureEventPublish) {
		return errors.New("宿主未协商插件事件发布能力")
	}
	if event == nil || event.Id == "" || event.Type == "" {
		return errors.New("事件 id 和 type 不能为空")
	}
	if !p.Limits.Normalize().PayloadAllowed(event.Payload) {
		return errors.New("事件 payload 超过协议上限")
	}
	return p.send(&pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_Event{
		Event: &pluginhostv1.EventEnvelope{Event: event},
	}})
}

// RegisterContribution 在运行期启用一条已被签名清单声明的贡献。
func (p *Plugin) RegisterContribution(ctx context.Context, contribution Contribution) error {
	// 先安装本地路由，再让宿主暴露路由，避免宿主 ACK 后的首个调用撞上空窗口。
	p.installLocalContribution(contribution)
	ack, err := p.updateContribution(ctx, &pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_Register{
		Register: &pluginhostv1.RegisterContributions{Contributions: []*pluginhostv1.Contribution{wireContribution(contribution)}},
	}})
	if err != nil {
		p.removeLocalContribution(contribution.ExtensionPoint, contribution.ID)
		return err
	}
	if len(ack.Rejected) > 0 {
		p.removeLocalContribution(contribution.ExtensionPoint, contribution.ID)
		return fmt.Errorf("宿主拒绝动态贡献: %v", ack.Rejected)
	}
	return nil
}

// UnregisterContribution 在运行期停用当前插件拥有的贡献。
func (p *Plugin) UnregisterContribution(ctx context.Context, extensionPoint, id string) error {
	ack, err := p.updateContribution(ctx, &pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_Unregister{
		Unregister: &pluginhostv1.UnregisterContributions{Contributions: []*pluginhostv1.ContributionRef{{
			ExtensionPoint: extensionPoint, Id: id,
		}}},
	}})
	if err != nil {
		return err
	}
	if len(ack.Rejected) > 0 {
		return fmt.Errorf("宿主拒绝动态卸载贡献: %v", ack.Rejected)
	}
	p.removeLocalContribution(extensionPoint, id)
	return nil
}

func (p *Plugin) installLocalContribution(contribution Contribution) {
	p.contribMu.Lock()
	defer p.contribMu.Unlock()
	p.contribs = append(p.contribs, contribution)
	for operation, handler := range contribution.Handlers {
		p.routes[routeKey(contribution.ExtensionPoint, contribution.ID, operation)] = handler
	}
}

func (p *Plugin) removeLocalContribution(extensionPoint, id string) {
	p.contribMu.Lock()
	defer p.contribMu.Unlock()
	filtered := p.contribs[:0]
	for _, contribution := range p.contribs {
		if contribution.ExtensionPoint != extensionPoint || contribution.ID != id {
			filtered = append(filtered, contribution)
		}
	}
	p.contribs = filtered
	prefix := extensionPoint + "|" + id + "|"
	for key := range p.routes {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(p.routes, key)
		}
	}
}

func (p *Plugin) updateContribution(ctx context.Context, message *pluginhostv1.FromPlugin) (*pluginhostv1.ContributionUpdateAck, error) {
	if !p.hasFeature(protocol.FeatureDynamicContributions) {
		return nil, errors.New("宿主未协商动态贡献能力")
	}
	reqID := fmt.Sprintf("cu-%d", p.seq.Add(1))
	switch typed := message.Msg.(type) {
	case *pluginhostv1.FromPlugin_Register:
		typed.Register.RequestId = reqID
	case *pluginhostv1.FromPlugin_Unregister:
		typed.Unregister.RequestId = reqID
	}
	ch := make(chan *pluginhostv1.FromHost, 1)
	p.pendingMu.Lock()
	if len(p.pending) >= p.Limits.Normalize().MaxPendingRequests {
		p.pendingMu.Unlock()
		return nil, errors.New("动态贡献 pending 请求队列已满")
	}
	p.pending[reqID] = ch
	p.pendingMu.Unlock()
	defer func() {
		p.pendingMu.Lock()
		delete(p.pending, reqID)
		p.pendingMu.Unlock()
	}()
	if err := p.send(message); err != nil {
		return nil, err
	}
	select {
	case response := <-ch:
		ack := response.GetContributionUpdateAck()
		if ack == nil {
			return nil, errors.New("动态贡献响应为空")
		}
		return ack, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func wireContribution(c Contribution) *pluginhostv1.Contribution {
	return &pluginhostv1.Contribution{ExtensionPoint: c.ExtensionPoint, Id: c.ID,
		Priority: c.Priority, DescriptorJson: c.Descriptor}
}

func pluginCallContext(ctx context.Context, callCtx *contractv1.CallContext, timeout time.Duration) (context.Context, *contractv1.CallContext, context.CancelFunc) {
	deadline := time.Now().Add(timeout)
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
	if inherited, ok := ctx.Value(invocationCallPathKey{}).([]string); ok {
		bounded.CallPath = append([]string(nil), inherited...)
	}
	deadlineUnixMs := deadline.UnixMilli()
	bounded.DeadlineUnixMs = &deadlineUnixMs
	boundedCtx, cancel := context.WithDeadline(ctx, deadline)
	return boundedCtx, bounded, cancel
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
