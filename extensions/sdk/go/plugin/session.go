package plugin

import (
	"context"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
)

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
