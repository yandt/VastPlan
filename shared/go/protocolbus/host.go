// Package protocolbus 实现宿主 ↔ 插件的协议总线（内核内通信）。
//
// 范围是内核内：一套内核宿主与它在本节点管辖的独立进程插件（ADR-0004）。
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
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	contractv1 "github.com/yandt/VastPlan/shared/go/contract/v1"
	pluginhostv1 "github.com/yandt/VastPlan/shared/go/pluginhost/v1"
	"github.com/yandt/VastPlan/shared/go/registry"
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

// PluginProcess 宿主侧持有的一个已接入插件。
type PluginProcess struct {
	PluginID  string
	Version   string
	SessionID string
}

// Host 插件宿主：拉起插件进程、握手、把贡献接入扩展点注册表、路由调用、探活。
type Host struct {
	// KernelName 本内核的规范 ID（backend/frontend/runner/mobile，ADR-0015）。
	KernelName string
	// KernelVersion 本内核 SemVer 版本，单一真源 = kernels/<name>/VERSION（ADR-0017 §1）。
	KernelVersion string

	Registry *registry.Registry
	Logf     func(format string, args ...any)

	// 时限（零值时用默认）。
	LaunchTimeout    time.Duration
	CallTimeout      time.Duration
	HeartbeatEvery   time.Duration
	HeartbeatTimeout time.Duration

	pluginhostv1.UnimplementedPluginHostServer

	srv  *grpc.Server
	lis  net.Listener
	addr string

	mu       sync.RWMutex
	sessions map[string]*session // sessionID → session
	byPlugin map[string]*session // pluginID  → session
	launches map[string]chan launchResult
	services map[string]HostService // 内核自身能力：capability → 实现

	stopped atomic.Bool
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
		KernelName:    kernelName,
		KernelVersion: kernelVersion,
		Registry:      r,
		Logf:          logf,
		sessions:      map[string]*session{},
		byPlugin:      map[string]*session{},
		launches:      map[string]chan launchResult{},
		services:      map[string]HostService{},
	}
}

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
	h.srv = grpc.NewServer()
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
	h.mu.RLock()
	sessions := make([]*session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.RUnlock()

	for _, s := range sessions {
		// 停服时逐个回收；单个插件关闭失败不影响其余（teardown 会强制杀进程）
		if err := h.Close(&PluginProcess{PluginID: s.pluginID, SessionID: s.id}); err != nil {
			h.Logf("回收插件 %s 时出错: %v", s.pluginID, err)
		}
	}
	if h.srv != nil {
		h.srv.Stop()
	}
}
