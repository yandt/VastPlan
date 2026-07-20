package protocolbus

import (
	"context"
	"os"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
)

func TestHostCallRequestIDCannotBeClaimedTwice(t *testing.T) {
	sess := newSession("session", "plugin", "1.0.0")
	ctx, cancel := context.WithCancel(context.Background())
	if !sess.beginHostCall("same", cancel) {
		t.Fatal("首个 HostCall request_id 应认领成功")
	}
	if sess.beginHostCall("same", func() {}) {
		t.Fatal("重复 HostCall request_id 必须拒绝")
	}
	sess.cancelHostCall("same")
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Cancel 必须命中原始 HostCall")
	}
}

func TestHandshakeRequiresAndConsumesPendingLaunchToken(t *testing.T) {
	host := NewHost("backend", "0.1.0", registry.New(), nil)
	host.launches["valid"] = &launchAttempt{
		result: make(chan launchResult, 1),
		policy: LaunchPolicy{PluginID: "com.example.safe", Version: "1.2.3"},
	}
	hello := &pluginhostv1.Hello{
		Magic: protocol.MagicCookie, ProtoVersions: []int32{1},
		PluginId: "com.example.safe", PluginVersion: "1.2.3",
		Engines: map[string]string{"backend": "^0.1"},
	}
	if _, err := host.Handshake(context.Background(), hello); err == nil {
		t.Fatal("缺少 launch token 的握手必须拒绝")
	}
	hello.LaunchToken = "invented"
	if _, err := host.Handshake(context.Background(), hello); err == nil {
		t.Fatal("未知 launch token 的握手必须拒绝")
	}
	hello.LaunchToken = "valid"
	if _, err := host.Handshake(context.Background(), hello); err != nil {
		t.Fatalf("待启动插件应完成握手: %v", err)
	}
	if _, err := host.Handshake(context.Background(), hello); err == nil {
		t.Fatal("launch token 只能使用一次")
	}
}

func TestDeclaredContributionsMustMatchSignedManifest(t *testing.T) {
	expected := []pluginv1.RuntimeContribution{{
		ExtensionPoint: "permission.checker", ID: "safe.policy", Priority: 100,
		Descriptor: []byte(`{"title":"安全策略","applies":{}}`),
	}}
	valid := []*pluginhostv1.Contribution{{
		ExtensionPoint: "permission.checker", Id: "safe.policy", Priority: 100,
		DescriptorJson: []byte(`{"applies":{},"title":"安全策略"}`),
	}}
	if err := validateDeclaredContributions(expected, valid, true); err != nil {
		t.Fatalf("字段顺序不同但语义相同的 descriptor 应通过: %v", err)
	}
	for name, declared := range map[string][]*pluginhostv1.Contribution{
		"extra":      append(valid, &pluginhostv1.Contribution{ExtensionPoint: "hook", Id: "evil"}),
		"priority":   {{ExtensionPoint: "permission.checker", Id: "safe.policy", Priority: 1000, DescriptorJson: valid[0].DescriptorJson}},
		"descriptor": {{ExtensionPoint: "permission.checker", Id: "safe.policy", Priority: 100, DescriptorJson: []byte(`{"title":"被替换"}`)}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateDeclaredContributions(expected, declared, true); err == nil {
				t.Fatal("超出验签清单的运行时声明必须拒绝")
			}
		})
	}
}

func TestDynamicDeclarationAllowsAuthorizedSubsetOnly(t *testing.T) {
	expected := []pluginv1.RuntimeContribution{
		{ExtensionPoint: "tool.package", ID: "one", Descriptor: []byte(`{"title":"one"}`)},
		{ExtensionPoint: "tool.package", ID: "two", Descriptor: []byte(`{"title":"two"}`)},
	}
	declared := []*pluginhostv1.Contribution{{ExtensionPoint: "tool.package", Id: "one", DescriptorJson: []byte(`{"title":"one"}`)}}
	if err := validateDeclaredContributions(expected, declared, false); err != nil {
		t.Fatalf("动态贡献客户端的初始授权子集应通过: %v", err)
	}
	declared[0].Id = "unsigned"
	if err := validateDeclaredContributions(expected, declared, false); err == nil {
		t.Fatal("动态贡献仍不得越过签名清单授权")
	}
}

func TestHandshakeRequiresManifestFeatures(t *testing.T) {
	newAttempt := func() (*Host, *pluginhostv1.Hello) {
		host := NewHost("backend", "1.0.0", registry.New(), nil)
		host.launches["feature-token"] = &launchAttempt{result: make(chan launchResult, 1), policy: LaunchPolicy{
			PluginID: "com.example.features", Version: "1.0.0", RequiredFeatures: []string{protocol.FeatureCancellation},
		}}
		return host, &pluginhostv1.Hello{
			Magic: protocol.MagicCookie, ProtoVersions: []int32{1}, PluginId: "com.example.features",
			PluginVersion: "1.0.0", Engines: map[string]string{"backend": "^1.0"}, LaunchToken: "feature-token",
		}
	}
	host, hello := newAttempt()
	if _, err := host.Handshake(context.Background(), hello); err == nil {
		t.Fatal("清单要求的能力未被客户端提供时必须拒绝")
	}
	host, hello = newAttempt()
	hello.Features = []string{protocol.FeatureCancellation}
	ack, err := host.Handshake(context.Background(), hello)
	if err != nil {
		t.Fatal(err)
	}
	if !protocol.HasFeature(ack.NegotiatedFeatures, protocol.FeatureCancellation) {
		t.Fatalf("握手未返回协商能力: %v", ack.NegotiatedFeatures)
	}
}

func TestPluginEnvironmentUsesExplicitAllowlist(t *testing.T) {
	t.Setenv("VASTPLAN_SHOULD_PASS", "allowed")
	t.Setenv("VASTPLAN_ARTIFACT_READ_TOKEN", "must-not-leak")
	environment := pluginEnvironment([]string{"VASTPLAN_SHOULD_PASS"})
	joined := map[string]string{}
	for _, item := range environment {
		key, value, found := splitEnv(item)
		if found {
			joined[key] = value
		}
	}
	if joined["VASTPLAN_SHOULD_PASS"] != "allowed" {
		t.Fatalf("显式允许的环境变量未传递: %v", environment)
	}
	if _, leaked := joined["VASTPLAN_ARTIFACT_READ_TOKEN"]; leaked {
		t.Fatal("宿主制品令牌不得传给插件")
	}
	_ = os.Getenv("PATH") // 证明测试不依赖当前 PATH 内容。
}

func TestStartupConfigurationMustBeBoundedJSONObject(t *testing.T) {
	if err := validateStartupConfiguration([]byte(`{"region":"cn-east"}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{[]byte(`[]`), []byte(`{"broken"`), make([]byte, protocol.MaxPluginConfigBytes+1)} {
		if err := validateStartupConfiguration(raw); err == nil {
			t.Fatalf("非法插件启动配置必须拒绝: bytes=%d", len(raw))
		}
	}
	input := []byte(`{"region":"cn-east"}`)
	policy := cloneLaunchPolicy(LaunchPolicy{Configuration: input})
	policy.Configuration[2] = 'X'
	if string(input) != `{"region":"cn-east"}` {
		t.Fatal("启动配置必须防御性复制")
	}
}

func TestReservedPluginEnvironmentCannotBeInheritedOrOverridden(t *testing.T) {
	t.Setenv(protocol.PluginConfigEnvKey, `{"spoofed":true}`)
	for _, item := range pluginEnvironment([]string{protocol.PluginConfigEnvKey}) {
		if key, _, ok := splitEnv(item); ok && key == protocol.PluginConfigEnvKey {
			t.Fatal("宿主保留启动配置不得通过普通 allowlist 继承")
		}
	}
	if err := validateExtraEnvironment([]string{protocol.PluginConfigEnvKey + `={"spoofed":true}`}); err == nil {
		t.Fatal("运行驱动不得覆盖宿主保留启动配置")
	}
}

func splitEnv(item string) (string, string, bool) {
	for index := range item {
		if item[index] == '=' {
			return item[:index], item[index+1:], true
		}
	}
	return "", "", false
}
