package protocolbus

import (
	"context"
	"errors"
	"fmt"

	pluginhostv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/protocol"
)

// Handshake 校验 magic、协商协议版本、校验 engines，通过后签发会话票据（§2.2）。
// 任一关不过即拒绝——fail-closed（ADR-0017 §4 强制点 1/2）。
func (h *Host) Handshake(ctx context.Context, in *pluginhostv1.Hello) (*pluginhostv1.HelloAck, error) {
	if in == nil {
		return nil, errors.New("握手请求不能为空")
	}
	fail := func(err error) (*pluginhostv1.HelloAck, error) {
		// 把失败原因回传给正在等待的 Launch，否则它只能看到"插件退出"这种无用信息
		h.failLaunch(in.LaunchToken, err)
		return nil, err
	}

	if in.Magic != protocol.MagicCookie {
		return fail(errors.New("magic cookie 不匹配"))
	}
	policy, err := h.claimLaunch(in.LaunchToken, in.PluginId, in.PluginVersion)
	if err != nil {
		return fail(err)
	}

	negotiated := protocol.Negotiate(in.ProtoVersions, protocol.SupportedVersions)
	if negotiated < 0 {
		return fail(fmt.Errorf("协议版本无交集：插件 %v，宿主支持 %v",
			in.ProtoVersions, protocol.SupportedVersions))
	}
	negotiatedFeatures := protocol.NegotiateFeatures(in.Features, protocol.SupportedFeatures)
	for _, required := range policy.RequiredFeatures {
		if !protocol.HasFeature(negotiatedFeatures, required) {
			return fail(fmt.Errorf("插件要求的协议能力 %q 未协商成功", required))
		}
	}

	// engines：本内核版本须满足插件声明的 SemVer 范围；未声明本内核亦拒绝
	if err := protocol.CheckEngine(h.KernelName, h.KernelVersion, in.Engines[h.KernelName]); err != nil {
		return fail(fmt.Errorf("插件 %s@%s 与内核不兼容: %w", in.PluginId, in.PluginVersion, err))
	}

	trustedVersion := in.PluginVersion
	if policy.Version != "" {
		trustedVersion = policy.Version
	}
	sess := newSession(newSessionID(), in.PluginId, trustedVersion)
	sess.policy = policy
	for _, feature := range negotiatedFeatures {
		sess.features[feature] = true
	}
	h.mu.Lock()
	h.sessions[sess.id] = sess
	h.mu.Unlock()

	h.Logf("协议版本已协商 v%d，插件=%s@%s，session=%s",
		negotiated, in.PluginId, trustedVersion, sess.id)
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
		NegotiatedFeatures: negotiatedFeatures,
	}, nil
}

func (h *Host) claimLaunch(token, pluginID, version string) (LaunchPolicy, error) {
	if token == "" {
		return LaunchPolicy{}, errors.New("插件缺少一次性 launch token")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	attempt, ok := h.launches[token]
	if !ok || attempt.claimed {
		return LaunchPolicy{}, errors.New("launch token 无效、已使用或已过期")
	}
	if attempt.policy.PluginID != "" && attempt.policy.PluginID != pluginID {
		return LaunchPolicy{}, fmt.Errorf("插件身份与验签清单不一致: 期望 %s，实际 %s", attempt.policy.PluginID, pluginID)
	}
	if attempt.policy.Version != "" {
		if _, err := protocol.ResolveHandshakeVersion(version, attempt.policy.Version, attempt.policy.ArtifactChannel); err != nil {
			return LaunchPolicy{}, fmt.Errorf("插件版本与验签清单不一致: 期望 %s，实际 %s: %w", attempt.policy.Version, version, err)
		}
	}
	attempt.claimed = true
	return cloneLaunchPolicy(attempt.policy), nil
}

// Channel 运行态双向流：接收插件消息并按类型分发；宿主经 session.send 下发。
// 本函数在插件断开前不返回——它就是该插件的生命线。
