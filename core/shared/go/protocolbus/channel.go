package protocolbus

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/protocol"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
	"google.golang.org/grpc/metadata"
)

func (h *Host) Channel(stream pluginhostv1.PluginHost_ChannelServer) error {
	sess, err := h.sessionFromStream(stream)
	if err != nil {
		return err
	}
	if !sess.claimStream(stream) {
		return errors.New("会话 Channel 已被认领")
	}

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
	if err := validateDeclaredContributions(sess.policy.Contributions, decl.Contributions,
		!sess.hasFeature(protocol.FeatureDynamicContributions)); err != nil {
		return err
	}
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

func validateDeclaredContributions(expected []pluginv1.RuntimeContribution, declared []*pluginhostv1.Contribution, requireExact bool) error {
	if expected == nil {
		return nil // 仅兼容直接二进制演示；生产 Node Agent 始终传入签名清单策略。
	}
	if requireExact && len(expected) != len(declared) {
		return fmt.Errorf("运行时贡献数量与验签清单不一致: 期望 %d，实际 %d", len(expected), len(declared))
	}
	want := make(map[string]pluginv1.RuntimeContribution, len(expected))
	for _, contribution := range expected {
		want[contribution.ExtensionPoint+"\x00"+contribution.ID] = contribution
	}
	seen := make(map[string]struct{}, len(declared))
	for _, contribution := range declared {
		if contribution == nil {
			return errors.New("运行时贡献不能为空")
		}
		key := contribution.ExtensionPoint + "\x00" + contribution.Id
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("运行时贡献重复: %s/%s", contribution.ExtensionPoint, contribution.Id)
		}
		seen[key] = struct{}{}
		expectedContribution, ok := want[key]
		if !ok {
			return fmt.Errorf("运行时声明了验签清单未授权的贡献: %s/%s", contribution.ExtensionPoint, contribution.Id)
		}
		if expectedContribution.Priority != contribution.Priority || !sameDescriptor(expectedContribution.Descriptor, contribution.DescriptorJson) {
			return fmt.Errorf("运行时贡献与验签清单不一致: %s/%s", contribution.ExtensionPoint, contribution.Id)
		}
	}
	return nil
}

func sameDescriptor(left, right []byte) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	canonicalA, _ := json.Marshal(a)
	canonicalB, _ := json.Marshal(b)
	return bytes.Equal(canonicalA, canonicalB)
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
	sess.autonomousActive.Store(true)
	h.Logf("插件已激活 %s@%s", sess.pluginID, sess.pluginVersion)
	return nil
}

// lifecycle 下发生命周期指令并等待 Ack。
