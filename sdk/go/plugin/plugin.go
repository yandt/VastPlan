// Package plugin 是第一方插件开发 SDK 的 Go 实现（backend 面）。
//
// 插件只需：声明贡献 + 实现处理器，SDK 负责协议细节（握手/声明/生命周期/地址回报）。
// 协议规格见 docs/dev/architecture/插件契约与协议.md 第二章。
package plugin

import (
	"context"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc"

	contractv1 "github.com/yandt/VastPlan/shared/go/contract/v1"
	pluginhostv1 "github.com/yandt/VastPlan/shared/go/pluginhost/v1"
)

// MagicCookie 必须与宿主一致，否则握手被拒（fail-closed）。
const MagicCookie = "VASTPLAN_PLUGIN_V1"

// ProtocolVersion 本 SDK 支持的协议版本集。
var ProtocolVersion = []int32{1}

// Handler 处理一次扩展点调用：收 CallContext + payload，回 CallResult + payload。
type Handler func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error)

// Contribution 插件对某扩展点的一条贡献。
type Contribution struct {
	ExtensionPoint string // 如 tool.package
	ID             string // 稳定逻辑名（= 清单 id = CallTarget.capability）
	Priority       int32
	Descriptor     []byte // 该扩展点的贡献契约（JSON，见第四章）
	// Handler 按 operation 分发；key "" 为默认处理器
	Handlers map[string]Handler
}

// Plugin 一个插件进程。
type Plugin struct {
	ID      string
	Version string

	contribs []Contribution
	// (extensionPoint, id, operation) -> Handler
	routes map[string]Handler
}

func New(id, version string) *Plugin {
	return &Plugin{ID: id, Version: version, routes: map[string]Handler{}}
}

// Contribute 登记一条贡献（在 Serve 前调用）。
func (p *Plugin) Contribute(c Contribution) {
	p.contribs = append(p.contribs, c)
	for op, h := range c.Handlers {
		p.routes[routeKey(c.ExtensionPoint, c.ID, op)] = h
	}
}

func routeKey(ep, id, op string) string { return ep + "|" + id + "|" + op }

// Serve 启动插件 gRPC 服务并把监听地址经 stdout 回报宿主，然后阻塞。
func (p *Plugin) Serve() error {
	// magic 校验：宿主经 env 注入，防止被当普通程序误启
	if os.Getenv("VASTPLAN_PLUGIN_MAGIC") != MagicCookie {
		return fmt.Errorf("magic cookie 不匹配：本程序是 VastPlan 插件，须由宿主拉起")
	}

	lis, err := net.Listen("tcp", "127.0.0.1:0") // 仅本机（协议总线范围是内核内）
	if err != nil {
		return fmt.Errorf("监听失败: %w", err)
	}

	srv := grpc.NewServer()
	pluginhostv1.RegisterPluginHostServer(srv, &server{p: p})

	// 回报监听地址（宿主经 stdout 读取，go-plugin 同款握手）
	fmt.Printf("VASTPLAN_PLUGIN_ADDR|%s\n", lis.Addr().String())
	os.Stdout.Sync()

	return srv.Serve(lis)
}

// server 实现 PluginHost 服务（插件侧）。
type server struct {
	pluginhostv1.UnimplementedPluginHostServer
	p         *Plugin
	sessionID string
	active    bool
}

func (s *server) Handshake(ctx context.Context, in *pluginhostv1.Hello) (*pluginhostv1.HelloAck, error) {
	if in.Magic != MagicCookie {
		return nil, fmt.Errorf("magic cookie 不匹配")
	}
	// 版本协商：取交集里最高的；无交集则拒绝（fail-closed）
	best := int32(-1)
	for _, hv := range in.ProtoVersions {
		for _, pv := range ProtocolVersion {
			if hv == pv && hv > best {
				best = hv
			}
		}
	}
	if best < 0 {
		return nil, fmt.Errorf("协议版本无交集：宿主 %v，插件 %v", in.ProtoVersions, ProtocolVersion)
	}
	s.sessionID = in.SessionId
	return &pluginhostv1.HelloAck{
		NegotiatedProto: best,
		PluginId:        s.p.ID,
		PluginVersion:   s.p.Version,
	}, nil
}

func (s *server) Declare(ctx context.Context, in *pluginhostv1.DeclareRequest) (*pluginhostv1.Declaration, error) {
	if in.SessionId != s.sessionID {
		return nil, fmt.Errorf("会话票据不匹配")
	}
	out := &pluginhostv1.Declaration{}
	for _, c := range s.p.contribs {
		out.Contributions = append(out.Contributions, &pluginhostv1.Contribution{
			ExtensionPoint: c.ExtensionPoint,
			Id:             c.ID,
			Priority:       c.Priority,
			DescriptorJson: c.Descriptor,
		})
	}
	return out, nil
}

func (s *server) Invoke(ctx context.Context, in *pluginhostv1.InvokeRequest) (*pluginhostv1.InvokeResponse, error) {
	if !s.active {
		return errResult("plugin.inactive", "插件未激活", false), nil
	}
	op := ""
	if in.Target.Operation != nil {
		op = *in.Target.Operation
	}
	h, ok := s.p.routes[routeKey(in.Target.ExtensionPoint, in.Target.Capability, op)]
	if !ok {
		// 回退到默认处理器
		h, ok = s.p.routes[routeKey(in.Target.ExtensionPoint, in.Target.Capability, "")]
	}
	if !ok {
		return errResult("capability.not_found",
			fmt.Sprintf("未实现 %s/%s 的操作 %q", in.Target.ExtensionPoint, in.Target.Capability, op), false), nil
	}

	res, payload, err := h(ctx, in.Context, in.Payload)
	if err != nil {
		// 应用层错误进 CallResult，不与传输层错误混淆（第二章 §2.7）
		return errResult("plugin.handler_error", err.Error(), true), nil
	}
	return &pluginhostv1.InvokeResponse{Result: res, Payload: payload}, nil
}

func (s *server) Lifecycle(ctx context.Context, in *pluginhostv1.Lifecycle) (*pluginhostv1.LifecycleAck, error) {
	switch in.Op {
	case pluginhostv1.Lifecycle_OP_ACTIVATE:
		s.active = true
		return &pluginhostv1.LifecycleAck{Ready: true}, nil
	case pluginhostv1.Lifecycle_OP_DEACTIVATE, pluginhostv1.Lifecycle_OP_DRAIN:
		s.active = false
		return &pluginhostv1.LifecycleAck{Ready: true}, nil
	case pluginhostv1.Lifecycle_OP_SHUTDOWN:
		s.active = false
		go func() { os.Exit(0) }() // 优雅退出
		return &pluginhostv1.LifecycleAck{Ready: true}, nil
	}
	msg := "未知生命周期指令"
	return &pluginhostv1.LifecycleAck{Ready: false, Message: &msg}, nil
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
