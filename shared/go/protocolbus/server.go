package protocolbus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc/metadata"

	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/shared/go/protocol"
	"cdsoft.com.cn/VastPlan/shared/go/registry"
)

// Handshake 校验 magic、协商协议版本、校验 engines，通过后签发会话票据（§2.2）。
// 任一关不过即拒绝——fail-closed（ADR-0017 §4 强制点 1/2）。
func (h *Host) Handshake(ctx context.Context, in *pluginhostv1.Hello) (*pluginhostv1.HelloAck, error) {
	fail := func(err error) (*pluginhostv1.HelloAck, error) {
		// 把失败原因回传给正在等待的 Launch，否则它只能看到"插件退出"这种无用信息
		h.failLaunch(in.LaunchToken, err)
		return nil, err
	}

	if in.Magic != protocol.MagicCookie {
		return fail(errors.New("magic cookie 不匹配"))
	}

	negotiated := protocol.Negotiate(in.ProtoVersions, protocol.SupportedVersions)
	if negotiated < 0 {
		return fail(fmt.Errorf("协议版本无交集：插件 %v，宿主支持 %v",
			in.ProtoVersions, protocol.SupportedVersions))
	}

	// engines：本内核版本须满足插件声明的 SemVer 范围；未声明本内核亦拒绝
	if err := protocol.CheckEngine(h.KernelName, h.KernelVersion, in.Engines[h.KernelName]); err != nil {
		return fail(fmt.Errorf("插件 %s@%s 与内核不兼容: %w", in.PluginId, in.PluginVersion, err))
	}

	sess := newSession(newSessionID(), in.PluginId, in.PluginVersion)
	h.mu.Lock()
	h.sessions[sess.id] = sess
	h.mu.Unlock()

	h.Logf("协议版本已协商 v%d，插件=%s@%s，session=%s",
		negotiated, in.PluginId, in.PluginVersion, sess.id)
	h.Logf("engines 校验通过：内核 %s@%s 满足插件要求 %q",
		h.KernelName, h.KernelVersion, in.Engines[h.KernelName])

	// 记住 launch_token，待 Channel 建立并激活后再回报 Launch
	h.mu.Lock()
	sess.launchToken = in.LaunchToken
	h.mu.Unlock()

	return &pluginhostv1.HelloAck{
		NegotiatedProto: negotiated,
		SessionId:       sess.id,
		HostCapabilities: []string{
			fmt.Sprintf("kernel=%s@%s", h.KernelName, h.KernelVersion),
		},
	}, nil
}

// Channel 运行态双向流：接收插件消息并按类型分发；宿主经 session.send 下发。
// 本函数在插件断开前不返回——它就是该插件的生命线。
func (h *Host) Channel(stream pluginhostv1.PluginHost_ChannelServer) error {
	sess, err := h.sessionFromStream(stream)
	if err != nil {
		return err
	}
	sess.stream = stream

	defer h.teardown(sess, errors.New("插件连接已断开"))

	// 首条消息必须是贡献声明（§2.2 时序）
	first, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("等待贡献声明失败: %w", err)
	}
	decl := first.GetDeclare()
	if decl == nil {
		return errors.New("首条消息必须是贡献声明")
	}
	sess.touch()

	if err := h.registerContributions(sess, decl); err != nil {
		return err
	}

	// 激活必须在读循环**启动之后**进行：它发出 Lifecycle 后要等 LifecycleAck，
	// 而 Ack 只能由下面的读循环收到——在此同步等待会自我死锁。
	go func() {
		if err := h.activate(sess); err != nil {
			h.teardown(sess, err)
			return
		}
		go h.heartbeat(sess)
		h.readyLaunch(sess) // 激活成功才算接入完成，此时 Launch 才返回
	}()

	// 读循环：任何一条消息都算插件活着
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil // 插件优雅退出
			}
			return err // 崩溃/断连 → defer teardown 摘除贡献
		}
		sess.touch()
		h.dispatch(sess, msg)
	}
}

// sessionFromStream 从 gRPC metadata 取会话票据并认领对应会话。
func (h *Host) sessionFromStream(stream pluginhostv1.PluginHost_ChannelServer) (*session, error) {
	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return nil, errors.New("缺少 metadata：无法确定会话")
	}
	vals := md.Get(protocol.SessionMetadataKey)
	if len(vals) == 0 {
		return nil, fmt.Errorf("缺少会话票据（metadata %q）", protocol.SessionMetadataKey)
	}
	h.mu.RLock()
	sess, ok := h.sessions[vals[0]]
	h.mu.RUnlock()
	if !ok {
		return nil, errors.New("会话票据无效或已过期")
	}
	return sess, nil
}

// registerContributions 把插件声明的贡献接入扩展点注册表（fail-closed：非法者拒绝）。
func (h *Host) registerContributions(sess *session, decl *pluginhostv1.Declaration) error {
	accepted := make([]string, 0, len(decl.Contributions))
	rejected := map[string]string{}

	for _, c := range decl.Contributions {
		// 注册时再次走正式 JSON Schema：清单是发布阶段的声明真源，
		// 而协议消息来自正在运行的进程，二者都必须防止 descriptor 漂移。
		if err := pluginv1.ValidateDescriptor(c.ExtensionPoint, c.DescriptorJson); err != nil {
			rejected[c.Id] = err.Error()
			h.Logf("贡献被拒 %s (%s): %v", c.Id, c.ExtensionPoint, err)
			continue
		}
		err := h.Registry.Register(registry.Contribution{
			ExtensionPoint: c.ExtensionPoint,
			ID:             c.Id,
			PluginID:       sess.pluginID,
			Priority:       int(c.Priority),
			Descriptor:     c.DescriptorJson,
		})
		if err != nil {
			rejected[c.Id] = err.Error()
			h.Logf("贡献被拒 %s (%s): %v", c.Id, c.ExtensionPoint, err)
			continue
		}
		accepted = append(accepted, c.Id)
		h.Logf("贡献已注册 %s → 扩展点 %s", c.Id, c.ExtensionPoint)
	}
	h.Logf("贡献注册完成：接受 %d，拒绝 %d", len(accepted), len(rejected))

	h.mu.Lock()
	h.byPlugin[sess.pluginID] = sess
	h.mu.Unlock()

	return sess.send(&pluginhostv1.FromHost{
		Msg: &pluginhostv1.FromHost_Registered{
			Registered: &pluginhostv1.Registered{Accepted: accepted, Rejected: rejected},
		},
	})
}

func (h *Host) activate(sess *session) error {
	ack, err := h.lifecycle(sess.stream.Context(), sess, pluginhostv1.Lifecycle_OP_ACTIVATE)
	if err != nil {
		return fmt.Errorf("激活失败: %w", err)
	}
	if !ack.Ready {
		msg := ""
		if ack.Message != nil {
			msg = *ack.Message
		}
		return fmt.Errorf("插件拒绝激活: %s", msg)
	}
	h.Logf("插件已激活 %s@%s", sess.pluginID, sess.pluginVersion)
	return nil
}

// lifecycle 下发生命周期指令并等待 Ack。
func (h *Host) lifecycle(ctx context.Context, sess *session, op pluginhostv1.Lifecycle_Op) (*pluginhostv1.LifecycleAck, error) {
	reqID := sess.nextRequestID()
	ch := sess.await(reqID)
	defer sess.release(reqID)

	if err := sess.send(&pluginhostv1.FromHost{
		Msg: &pluginhostv1.FromHost_Lifecycle{
			Lifecycle: &pluginhostv1.Lifecycle{RequestId: reqID, Op: op},
		},
	}); err != nil {
		return nil, err
	}

	select {
	case msg, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("插件已失联: %w", sess.err())
		}
		return msg.GetLifecycleAck(), nil
	case <-time.After(h.callTimeout()):
		return nil, errors.New("等待生命周期 Ack 超时")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// dispatch 按类型分发插件发来的消息。
func (h *Host) dispatch(sess *session, msg *pluginhostv1.FromPlugin) {
	switch m := msg.Msg.(type) {
	case *pluginhostv1.FromPlugin_InvokeResult:
		sess.deliver(m.InvokeResult.RequestId, msg)
	case *pluginhostv1.FromPlugin_LifecycleAck:
		sess.deliver(m.LifecycleAck.RequestId, msg)
	case *pluginhostv1.FromPlugin_Pong:
		sess.deliver(m.Pong.RequestId, msg)
	case *pluginhostv1.FromPlugin_HostCall:
		go h.serveHostCall(sess, m.HostCall) // 不阻塞读循环
	case *pluginhostv1.FromPlugin_Event:
		h.Logf("收到插件事件 type=%s source=%s", m.Event.Event.Type, m.Event.Event.Source)
	default:
		h.Logf("忽略未知消息类型 %T", m)
	}
}

// heartbeat 周期性 Ping；连续无响应即判定失联并摘除其贡献（§2.6 心跳/崩溃）。
func (h *Host) heartbeat(sess *session) {
	every := h.HeartbeatEvery
	if every <= 0 {
		every = defaultHeartbeatEvery
	}
	timeout := h.HeartbeatTimeout
	if timeout <= 0 {
		timeout = defaultHeartbeatTimeout
	}

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-sess.done:
			return
		case <-ticker.C:
			if sess.idleFor() > timeout {
				h.Logf("插件 %s 心跳超时（%v 无任何消息），判定失联", sess.pluginID, sess.idleFor())
				h.teardown(sess, fmt.Errorf("心跳超时（%v）", timeout))
				return
			}
			reqID := sess.nextRequestID()
			ch := sess.await(reqID)
			if err := sess.send(&pluginhostv1.FromHost{
				Msg: &pluginhostv1.FromHost_Ping{Ping: &pluginhostv1.Ping{RequestId: reqID}},
			}); err != nil {
				sess.release(reqID)
				return // 发不出去 → 流已断，读循环的 teardown 会处理
			}
			select {
			case <-ch: // 收到 Pong
			case <-time.After(timeout):
				// 不在此直接判死：交给上面的 idleFor 判据，避免与其他消息竞态
			case <-sess.done:
			}
			sess.release(reqID)
		}
	}
}

// teardown 会话终结：摘除其全部贡献、唤醒在途等待者、回收进程（ADR-0004 故障隔离）。
func (h *Host) teardown(sess *session, cause error) {
	sess.teardownOnce.Do(func() {
		defer close(sess.teardownDone)
		sess.markDead(cause)

		if n := h.Registry.UnregisterPlugin(sess.pluginID); n > 0 {
			h.Logf("已摘除插件 %s 的 %d 条贡献（原因: %v）", sess.pluginID, n, cause)
		}

		h.mu.Lock()
		delete(h.sessions, sess.id)
		if cur, ok := h.byPlugin[sess.pluginID]; ok && cur == sess {
			delete(h.byPlugin, sess.pluginID)
		}
		h.mu.Unlock()

		h.failLaunch(sess.launchToken, cause) // 若仍在 Launch 等待中，让它立刻脱身
		sess.killProcess()
	})
	// done 只表示流已死亡；teardownDone 才证明贡献、会话表和进程已经全部收敛。
	// Close/Stop 的调用者据此获得同步完成语义，不再与读循环的 defer teardown 竞态。
	<-sess.teardownDone
}
