// Package protocolbus 实现宿主 ↔ 插件的协议总线（内核内通信）。
//
// 范围是内核内：一套内核宿主与它在本节点管辖的独立进程插件（ADR-0004）。
// 跨服务/跨机器不归本协议（走寻址层 + NATS，系统架构 第二章）。
// 规格见 docs/dev/architecture/插件契约与协议.md 第二章。
package protocolbus

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	contractv1 "github.com/yandt/VastPlan/shared/go/contract/v1"
	pluginhostv1 "github.com/yandt/VastPlan/shared/go/pluginhost/v1"
	"github.com/yandt/VastPlan/shared/go/protocol"
	"github.com/yandt/VastPlan/shared/go/registry"
)

// PluginProcess 宿主侧持有的一个已接入插件。
type PluginProcess struct {
	PluginID  string
	Version   string
	SessionID string

	cmd    *exec.Cmd
	conn   *grpc.ClientConn
	client pluginhostv1.PluginHostClient
}

// Host 插件宿主：拉起插件进程、握手、把贡献接入扩展点注册表、路由调用。
type Host struct {
	// KernelName 本内核的规范 ID（backend/frontend/runner/mobile，ADR-0015）。
	KernelName string
	// KernelVersion 本内核 SemVer 版本，单一真源 = kernels/<name>/VERSION（ADR-0017 §1）。
	KernelVersion string

	Registry *registry.Registry
	Logf     func(format string, args ...any)
}

func NewHost(kernelName, kernelVersion string, r *registry.Registry, logf func(string, ...any)) *Host {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Host{KernelName: kernelName, KernelVersion: kernelVersion, Registry: r, Logf: logf}
}

// Launch 拉起插件进程并完成握手 + 贡献注册 + 激活。
//
// 流程（第二章 §2.2）：
//
//	拉起进程（注入 magic cookie）→ 插件回报监听地址 → Hello/HelloAck 协商版本
//	→ RegisterContributions → 注册进 Registry → Lifecycle{ACTIVATE}
func (h *Host) Launch(ctx context.Context, binPath string) (*PluginProcess, error) {
	cmd := exec.Command(binPath)
	cmd.Env = append(cmd.Environ(), protocol.MagicEnvKey+"="+protocol.MagicCookie)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("接管插件 stdout: %w", err)
	}
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("拉起插件进程: %w", err)
	}

	// 插件把监听地址经 stdout 回报宿主（go-plugin 同款握手）
	addr, err := readAddr(stdout)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	h.Logf("插件进程已启动 pid=%d addr=%s", cmd.Process.Pid, addr)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("连接插件: %w", err)
	}
	client := pluginhostv1.NewPluginHostClient(conn)

	// 1) 握手：magic 校验 + 版本协商 + 下发会话票据（ADR-0017 §4 强制点 1）
	sessionID := newSessionID()
	ack, err := client.Handshake(ctx, &pluginhostv1.Hello{
		ProtoVersions: protocol.SupportedVersions,
		Magic:         protocol.MagicCookie,
		SessionId:     sessionID,
	})
	if err != nil {
		_ = conn.Close()
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("握手失败: %w", err)
	}
	// 无交集即拒绝（fail-closed）
	if !protocol.Supports(ack.NegotiatedProto) {
		_ = conn.Close()
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("协议版本无交集：插件回 %d，宿主支持 %v", ack.NegotiatedProto, protocol.SupportedVersions)
	}

	// 2) engines 校验：本内核版本须满足插件声明的 SemVer 范围（ADR-0017 §4 强制点 2）
	if err := protocol.CheckEngine(h.KernelName, h.KernelVersion, ack.Engines[h.KernelName]); err != nil {
		_ = conn.Close()
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("插件 %s@%s 与内核不兼容: %w", ack.PluginId, ack.PluginVersion, err)
	}

	p := &PluginProcess{
		PluginID:  ack.PluginId,
		Version:   ack.PluginVersion,
		SessionID: sessionID,
		cmd:       cmd, conn: conn, client: client,
	}
	h.Logf("协议版本已协商 v%d，插件=%s@%s，session=%s",
		ack.NegotiatedProto, ack.PluginId, ack.PluginVersion, sessionID)
	h.Logf("engines 校验通过：内核 %s@%s 满足插件要求 %q",
		h.KernelName, h.KernelVersion, ack.Engines[h.KernelName])

	// 3) 贡献声明：插件声明它填充哪些扩展点
	decl, err := client.Declare(ctx, &pluginhostv1.DeclareRequest{SessionId: sessionID})
	if err != nil {
		_ = h.Close(p)
		return nil, fmt.Errorf("拉取贡献声明失败: %w", err)
	}

	// 4) 接入扩展点注册表（非法者拒绝，fail-closed；ADR-0017 §4 强制点 3）
	accepted, rejected := 0, 0
	for _, c := range decl.Contributions {
		err := h.Registry.Register(registry.Contribution{
			ExtensionPoint: c.ExtensionPoint,
			ID:             c.Id,
			PluginID:       p.PluginID,
			Priority:       int(c.Priority),
			Descriptor:     c.DescriptorJson,
		})
		if err != nil {
			rejected++
			h.Logf("贡献被拒 %s (%s): %v", c.Id, c.ExtensionPoint, err)
			continue
		}
		accepted++
		h.Logf("贡献已注册 %s → 扩展点 %s", c.Id, c.ExtensionPoint)
	}
	h.Logf("贡献注册完成：接受 %d，拒绝 %d", accepted, rejected)

	// 5) 激活
	if _, err := client.Lifecycle(ctx, &pluginhostv1.Lifecycle{Op: pluginhostv1.Lifecycle_OP_ACTIVATE}); err != nil {
		_ = h.Close(p)
		return nil, fmt.Errorf("激活失败: %w", err)
	}
	h.Logf("插件已激活 %s@%s", p.PluginID, p.Version)
	return p, nil
}

// Invoke 扩展点被触发时，宿主把 CallContext 经总线转给插件，收 CallResult。
// 先查注册表解析能力（本地命中），再路由到提供它的插件。
func (h *Host) Invoke(ctx context.Context, p *PluginProcess, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*pluginhostv1.InvokeResponse, error) {

	c, ok := h.Registry.Lookup(target.ExtensionPoint, target.Capability)
	if !ok {
		return nil, fmt.Errorf("能力未注册：%s/%s", target.ExtensionPoint, target.Capability)
	}
	if c.PluginID != p.PluginID {
		return nil, fmt.Errorf("能力 %s 由插件 %s 提供，非 %s", target.Capability, c.PluginID, p.PluginID)
	}
	return p.client.Invoke(ctx, &pluginhostv1.InvokeRequest{
		Target: target, Context: callCtx, Payload: payload,
	})
}

// Close 优雅关闭插件：SHUTDOWN 指令 → 摘除贡献 → 断连 → 回收进程。
// 插件崩溃时同样摘除其贡献（ADR-0004 故障隔离）。
func (h *Host) Close(p *PluginProcess) error {
	if p.client != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = p.client.Lifecycle(shutdownCtx, &pluginhostv1.Lifecycle{Op: pluginhostv1.Lifecycle_OP_SHUTDOWN})
		cancel()
	}
	if p.PluginID != "" {
		n := h.Registry.UnregisterPlugin(p.PluginID)
		h.Logf("已摘除插件 %s 的 %d 条贡献", p.PluginID, n)
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_, _ = p.cmd.Process.Wait()
	}
	return nil
}

// readAddr 读插件经 stdout 回报的监听地址，格式：VASTPLAN_PLUGIN_ADDR|<addr>
func readAddr(stdout interface{ Read([]byte) (int, error) }) (string, error) {
	type result struct {
		addr string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
				if a, ok := strings.CutPrefix(line, protocol.AddrPrefix); ok {
				ch <- result{addr: a}
				return
			}
		}
		ch <- result{err: fmt.Errorf("插件未回报监听地址（stdout 已结束）")}
	}()
	select {
	case r := <-ch:
		return r.addr, r.err
	case <-time.After(10 * time.Second):
		return "", fmt.Errorf("等待插件回报地址超时")
	}
}

// newSessionID 签发会话票据（宿主侧），用于审计与插件回调鉴权。
func newSessionID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return "sess-" + hex.EncodeToString(b)
}
