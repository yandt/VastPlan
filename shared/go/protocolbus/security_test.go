package protocolbus

import (
	"context"
	"os"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/shared/go/protocol"
	"cdsoft.com.cn/VastPlan/shared/go/registry"
)

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
	if err := validateDeclaredContributions(expected, valid); err != nil {
		t.Fatalf("字段顺序不同但语义相同的 descriptor 应通过: %v", err)
	}
	for name, declared := range map[string][]*pluginhostv1.Contribution{
		"extra":      append(valid, &pluginhostv1.Contribution{ExtensionPoint: "hook", Id: "evil"}),
		"priority":   {{ExtensionPoint: "permission.checker", Id: "safe.policy", Priority: 1000, DescriptorJson: valid[0].DescriptorJson}},
		"descriptor": {{ExtensionPoint: "permission.checker", Id: "safe.policy", Priority: 100, DescriptorJson: []byte(`{"title":"被替换"}`)}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateDeclaredContributions(expected, declared); err == nil {
				t.Fatal("超出验签清单的运行时声明必须拒绝")
			}
		})
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

func splitEnv(item string) (string, string, bool) {
	for index := range item {
		if item[index] == '=' {
			return item[:index], item[index+1:], true
		}
	}
	return "", "", false
}
