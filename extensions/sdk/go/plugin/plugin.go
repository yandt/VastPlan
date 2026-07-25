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

	"google.golang.org/grpc"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
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
