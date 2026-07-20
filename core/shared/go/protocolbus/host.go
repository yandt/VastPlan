// Package protocolbus 实现宿主 ↔ 插件的协议总线（内核内通信）。
//
// 范围是内核内：一套内核宿主与它在本节点管辖的进程/dynamic-go 插件实例（ADR-0051）。
// 跨服务/跨机器不归本协议（走寻址层 + NATS，系统架构 第二章）。
// 规格见 docs/dev/architecture/插件契约与协议.md 第二章。
//
// 方向：宿主是 gRPC 服务端，插件回连（§2.2）。插件→宿主的回调因此天然可行；
// 宿主→插件的调用经 Channel 双向流下发，用 request_id 关联请求与响应。
package protocolbus

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/observability"
	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/processguard"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocollimit"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
)

// randomHex 生成随机十六进制串（会话票据 / launch token 用）。
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败极罕见；退化为时间戳仍保证本进程内唯一
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// KernelPluginID 内核自身在注册表中的身份：内核直接提供的能力挂在它名下，
// 使插件回调宿主与调用别的插件共用同一套 capability 寻址（§2.4）。
const KernelPluginID = "__kernel__"

// 时限：均可经 Host 字段覆盖，便于测试注入短值（勿硬编码，见 host_internal_test）。
const (
	defaultLaunchTimeout    = 15 * time.Second
	defaultCallTimeout      = 30 * time.Second
	defaultHeartbeatEvery   = 5 * time.Second
	defaultHeartbeatTimeout = 15 * time.Second
)

// HostService 内核自身提供的能力实现（插件经 HostCall 回调它）。
type HostService func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error)

// CapabilityForwarder resolves a capability outside the current service unit.
// Backend Runtime binds it to the cluster addressing router; protocolbus keeps
// the interface transport-neutral and still runs local authorization first.
type CapabilityForwarder func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)

// LaunchPolicy 把已验证制品身份和签名清单授权绑定到一次插件启动。
type LaunchPolicy struct {
	PluginID          string
	Publisher         string
	Version           string
	ArtifactSHA256    string
	NodeID            string
	RuntimeInstanceID string
	Contributions     []pluginv1.RuntimeContribution
	KernelServices    []string
	ContextAccess     pluginv1.ContextAccess
	// ContextCeiling is the host-user/publisher policy result. Empty means the
	// host default; plugins cannot set or widen it through their manifest.
	ContextCeiling []string
	// UnrestrictedContext is only set by Host.Launch for an explicit local
	// development launch without a signed manifest. Production callers leave
	// it false and supply ContextAccess.
	UnrestrictedContext  bool
	EnvironmentAllowlist []string
	// Configuration is the already validated, plugin-isolated non-sensitive
	// JSON object injected into this logical runtime only.
	Configuration    []byte
	RequiredFeatures []string
	// RuntimeScope is trusted host-only placement metadata. It is not sent over
	// the wire or accepted from a plugin manifest; managed execution drivers use
	// it to keep pools within one kernel service instance.
	RuntimeScope string
	// RuntimeGeneration separates providers that cannot unload code (notably
	// dynamic-go) while a candidate and current service generation overlap.
	RuntimeGeneration string
}

// LaunchSpec 是运行驱动交给协议宿主的语言无关启动结果。Command/Args 直接传给
// os/exec，不经过 shell；Dir 和 ExtraEnv 由可信驱动生成，插件清单不能注入宿主票据。
type LaunchSpec struct {
	Command  string
	Args     []string
	Dir      string
	ExtraEnv []string
	// RuntimeKind 是受信任驱动写入的诊断标识，例如 process、node-worker、
	// python-subinterpreter。它不参与授权，也不能由插件环境覆盖。
	RuntimeKind string
}

type launchAttempt struct {
	result  chan launchResult
	policy  LaunchPolicy
	claimed bool
}

// MigrationOperation 是插件私有状态 copy-on-write 事务的三个可回滚阶段。
type MigrationOperation string

const (
	MigrationPrepare  MigrationOperation = "prepare"
	MigrationCommit   MigrationOperation = "commit"
	MigrationRollback MigrationOperation = "rollback"
)

type StateIdentity = pluginv1.StateIdentity

// MigrationCommand 是宿主发给候选插件的一次迁移阶段命令。
type MigrationCommand struct {
	Operation     MigrationOperation
	TransactionID string
	From          StateIdentity
	To            StateIdentity
}

// MigrationRequest 保留为源代码兼容别名；新代码使用语义更准确的 MigrationCommand。
type MigrationRequest = MigrationCommand

// PluginInstance 是宿主侧持有的一个已接入执行单元。它可以来自独立进程、
// Runtime Host 管理的 Worker/子解释器，或受控内嵌驱动；上层生命周期不区分语言。
type PluginInstance struct {
	PluginID    string
	Version     string
	SessionID   string
	PID         int
	runtimeKind string
	session     *session
	embedded    *embeddedInstance
}

// PluginProcess 是 v1 代码的源兼容别名。新代码使用 PluginInstance；协议和
// 运行态不会因为 Go 类型重命名发生 wire 变化。
type PluginProcess = PluginInstance

var closedProcessDone = func() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

// Done 在插件会话因崩溃、心跳超时或主动关闭而终结时关闭。
// Node Agent 依赖这个真实死亡信号，不能只凭启动记录判断进程仍健康。
func (p *PluginInstance) Done() <-chan struct{} {
	if p != nil && p.embedded != nil {
		return p.embedded.done
	}
	if p == nil || p.session == nil {
		return closedProcessDone
	}
	return p.session.done
}

// Err 返回会话终结原因；进程仍运行时为 nil。
func (p *PluginInstance) Err() error {
	if p != nil && p.embedded != nil {
		return p.embedded.terminalError()
	}
	if p == nil || p.session == nil {
		return nil
	}
	select {
	case <-p.session.done:
		return p.session.err()
	default:
		return nil
	}
}

// RuntimeKind 返回实例的运行形态，供状态与故障事件区分进程和内嵌实例。
func (p *PluginInstance) RuntimeKind() string {
	if p != nil && p.embedded != nil {
		if p.runtimeKind != "" {
			return p.runtimeKind
		}
		return "embedded"
	}
	if p != nil && p.runtimeKind != "" {
		return p.runtimeKind
	}
	return "process"
}

// Alive 同时检查会话是否仍连通，而非仅检查 PID 曾经存在。
func (p *PluginInstance) Alive() bool {
	select {
	case <-p.Done():
		return false
	default:
		return true
	}
}

// Host 插件宿主：接入插件实例、把贡献接入扩展点注册表、路由调用并管理生命周期。
type Host struct {
	// KernelName 本内核的规范 ID（backend/frontend/runner/mobile，ADR-0015）。
	KernelName string
	// KernelVersion 本内核 SemVer 版本，单一真源 = core/kernels/<name>/VERSION（ADR-0017 §1）。
	KernelVersion string

	Registry *registry.Registry
	Logf     func(format string, args ...any)
	Observer *observability.Observer
	// ProcessGuardian controls independently launched plugin process groups.
	// Nil selects the operating-system default.
	ProcessGuardian processguard.Guardian

	// 时限（零值时用默认）。
	LaunchTimeout    time.Duration
	CallTimeout      time.Duration
	HeartbeatEvery   time.Duration
	HeartbeatTimeout time.Duration
	// PluginEnvironmentAllowlist 是显式允许传给子进程的宿主环境变量名；默认空。
	PluginEnvironmentAllowlist []string
	// Limits 控制 payload、metadata、并发、pending 队列、默认 deadline 与 drain。
	// 零值字段使用 protocollimit 的统一安全默认，不能通过零值关闭保护。
	Limits protocollimit.Limits

	pluginhostv1.UnimplementedPluginHostServer

	srv  *grpc.Server
	lis  net.Listener
	addr string

	mu               sync.RWMutex
	sessions         map[string]*session          // sessionID → session
	byPlugin         map[string]*session          // pluginID  → session
	embedded         map[string]*embeddedInstance // instanceID → embedded instance
	embeddedByPlugin map[string]*embeddedInstance // pluginID → embedded instance
	launches         map[string]*launchAttempt
	services         map[string]HostService // 内核自身能力：capability → 实现
	forwarder        CapabilityForwarder

	stopped atomic.Bool

	callMu    sync.Mutex
	draining  bool
	inflight  int
	drainDone chan struct{}
	drainOnce sync.Once

	callbackMu    sync.Mutex
	callbackSlots chan struct{}
}

// SetCapabilityForwarder installs the service-to-service fallback. It is
// normally called before Start; replacing it while calls are in flight is safe.
func (h *Host) SetCapabilityForwarder(forwarder CapabilityForwarder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.forwarder = forwarder
}

type launchResult struct {
	sess *session
	err  error
}

func NewHost(kernelName, kernelVersion string, r *registry.Registry, logf func(string, ...any)) *Host {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Host{
		KernelName:       kernelName,
		KernelVersion:    kernelVersion,
		Registry:         r,
		Logf:             logf,
		Observer:         observability.New(nil, nil),
		sessions:         map[string]*session{},
		byPlugin:         map[string]*session{},
		embedded:         map[string]*embeddedInstance{},
		embeddedByPlugin: map[string]*embeddedInstance{},
		launches:         map[string]*launchAttempt{},
		services:         map[string]HostService{},
		drainDone:        make(chan struct{}),
		Limits:           protocollimit.Default(),
	}
}

func (h *Host) limits() protocollimit.Limits { return h.Limits.Normalize() }

func (h *Host) launchTimeout() time.Duration {
	if h.LaunchTimeout > 0 {
		return h.LaunchTimeout
	}
	return defaultLaunchTimeout
}

func (h *Host) callTimeout() time.Duration {
	if h.CallTimeout > 0 {
		return h.CallTimeout
	}
	return defaultCallTimeout
}

// RegisterHostService 登记一个内核自身提供的能力，并把它注册进扩展点注册表，
// 使插件可用与调用别的插件完全相同的方式（capability 寻址）回调它。
func (h *Host) RegisterHostService(extensionPoint, capability string, fn HostService) error {
	if err := h.Registry.Register(registry.Contribution{
		ExtensionPoint: extensionPoint,
		ID:             capability,
		PluginID:       KernelPluginID,
	}); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.services[capability] = fn
	return nil
}

// Start 开始监听并提供 PluginHost 服务。插件经注入的地址回连本宿主。
func (h *Host) Start() error {
	lis, err := net.Listen("tcp", "127.0.0.1:0") // 仅本机：协议总线范围是内核内
	if err != nil {
		return fmt.Errorf("宿主监听失败: %w", err)
	}
	h.lis = lis
	h.addr = lis.Addr().String()
	limits := h.limits()
	h.callbackMu.Lock()
	h.callbackSlots = make(chan struct{}, limits.MaxConcurrentCalls)
	h.callbackMu.Unlock()
	h.srv = grpc.NewServer(
		grpc.MaxRecvMsgSize(limits.MaxMessageBytes()),
		grpc.MaxSendMsgSize(limits.MaxMessageBytes()),
		grpc.MaxHeaderListSize(limits.MaxMetadataBytes),
		grpc.MaxConcurrentStreams(uint32(limits.MaxConcurrentCalls)),
	)
	pluginhostv1.RegisterPluginHostServer(h.srv, h)

	go func() {
		if err := h.srv.Serve(lis); err != nil && !h.stopped.Load() {
			h.Logf("宿主 gRPC 服务退出: %v", err)
		}
	}()
	h.Logf("宿主已监听 %s", h.addr)
	return nil
}

// Addr 宿主监听地址（Start 之后有效）。
func (h *Host) Addr() string { return h.addr }

// Stop 停服并回收全部插件。
func (h *Host) Stop() {
	h.stopped.Store(true)
	stopCtx, stopCancel := context.WithTimeout(context.Background(), h.limits().DrainTimeout)
	_ = h.waitForInflight(stopCtx)
	stopCancel()
	h.mu.RLock()
	sessions := make([]*session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	embedded := make([]*embeddedInstance, 0, len(h.embedded))
	for _, instance := range h.embedded {
		embedded = append(embedded, instance)
	}
	h.mu.RUnlock()

	// 并行回收所有执行单元，使总停机上界取决于最慢的一个插件，
	// 而不是把每个故障插件的宽限期累加到 systemd TimeoutStopSec。
	var closeGroup sync.WaitGroup
	for _, sess := range sessions {
		closeGroup.Add(1)
		go func(sess *session) {
			defer closeGroup.Done()
			if err := h.Close(&PluginProcess{PluginID: sess.pluginID, SessionID: sess.id}); err != nil {
				h.Logf("回收插件 %s 时出错: %v", sess.pluginID, err)
			}
		}(sess)
	}
	for _, instance := range embedded {
		closeGroup.Add(1)
		go func(instance *embeddedInstance) {
			defer closeGroup.Done()
			if err := h.Close(&PluginProcess{PluginID: instance.pluginID, Version: instance.version,
				SessionID: instance.id, embedded: instance}); err != nil {
				h.Logf("回收内嵌插件 %s 时出错: %v", instance.pluginID, err)
			}
		}(instance)
	}
	closeGroup.Wait()
	if h.srv != nil {
		h.srv.Stop()
	}
}

// Drain 让全部插件停止接收新调用，并等待已经进入处理器的调用完成。
// 调用方应先把新流量切到候选宿主，再 drain/stop 旧宿主。
func (h *Host) Drain(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.limits().DrainTimeout)
		defer cancel()
	}
	if err := h.waitForInflight(ctx); err != nil {
		return fmt.Errorf("等待宿主在途调用完成: %w", err)
	}
	h.mu.RLock()
	sessions := make([]*session, 0, len(h.sessions))
	for _, sess := range h.sessions {
		sessions = append(sessions, sess)
	}
	embedded := make([]*embeddedInstance, 0, len(h.embedded))
	for _, instance := range h.embedded {
		embedded = append(embedded, instance)
	}
	h.mu.RUnlock()

	errs := make(chan error, len(sessions)+len(embedded))
	var wg sync.WaitGroup
	for _, sess := range sessions {
		wg.Add(1)
		go func(sess *session) {
			defer wg.Done()
			if _, err := h.lifecycle(ctx, sess, pluginhostv1.Lifecycle_OP_DRAIN); err != nil {
				errs <- fmt.Errorf("drain 插件 %s: %w", sess.pluginID, err)
			}
		}(sess)
	}
	for _, instance := range embedded {
		wg.Add(1)
		go func(instance *embeddedInstance) {
			defer wg.Done()
			if err := instance.drain(ctx); err != nil {
				errs <- fmt.Errorf("drain 内嵌插件 %s: %w", instance.pluginID, err)
			}
		}(instance)
	}
	wg.Wait()
	close(errs)
	var joined error
	for err := range errs {
		joined = errors.Join(joined, err)
	}
	return joined
}
