package runtimehost

import (
	"context"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
)

func TestKernelServiceGrantPolicyRequiresManifestPlatformAndPublisherIntersection(t *testing.T) {
	policy := DefaultKernelServiceGrantPolicy()
	requested := []string{"kernel.state.shared.update", "kernel.state.shared.get"}
	granted, err := policy.Compile("cn.vastplan.settings", "vastplan", requested,
		[]string{"kernel.state.shared.get", "kernel.state.shared.update"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(granted) != 2 || granted[0] != "kernel.state.shared.get" || granted[1] != "kernel.state.shared.update" {
		t.Fatalf("授权结果未规范化: %#v", granted)
	}
	if _, err := policy.Compile("cn.vastplan.settings", "vastplan", requested, nil, false); err == nil {
		t.Fatal("缺失 Platform Grant 必须 fail-closed")
	}
	if _, err := policy.Compile("cn.vastplan.settings", "vastplan", requested,
		[]string{"kernel.state.shared.get"}, true); err == nil {
		t.Fatal("部分 Grant 不得产生运行期残缺插件")
	}
	if _, err := policy.Compile("cn.vastplan.settings", "vastplan", requested,
		[]string{"kernel.state.shared.get", "kernel.state.shared.update", "kernel.info"}, true); err == nil {
		t.Fatal("Platform Profile 不得授予 Manifest 未申请的服务")
	}
}

func TestKernelServiceGrantPolicyDeniesUnknownPublisherByDefault(t *testing.T) {
	policy := DefaultKernelServiceGrantPolicy()
	if _, err := policy.Compile("com.example.plugin", "example", []string{"kernel.info"}, []string{"kernel.info"}, true); err == nil {
		t.Fatal("未知发布者默认不得取得 Kernel Service")
	}
	configured, err := ParseKernelServiceGrantPolicy("", "example=kernel.info;vastplan=*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configured.Compile("com.example.plugin", "example", []string{"kernel.info"}, []string{"kernel.info"}, true); err != nil {
		t.Fatalf("显式发布者上限应允许精确交集: %v", err)
	}
}

func TestKernelServiceGrantPolicyRejectsInvalidRules(t *testing.T) {
	for _, rule := range []string{"example=", "example=kernel.info;example=kernel.config.get", "example=not-kernel"} {
		if _, err := ParseKernelServiceGrantPolicy("", rule); err == nil {
			t.Fatalf("无效规则应拒绝: %q", rule)
		}
	}
}

func TestStartPluginRejectsMissingPlatformGrantBeforeDriverLaunch(t *testing.T) {
	runtime := NewProtocolRuntime("1.0.0", nil)
	transaction := &applyTransaction{
		runtime:  runtime,
		unit:     RuntimeUnit{ID: "service-a"},
		envelope: pluginconfig.Envelope{Plugins: map[string]map[string]any{}},
	}
	plugin := InstalledPlugin{
		ID: "cn.vastplan.test", Publisher: "vastplan", Version: "1.0.0",
		Contract: PluginRuntimeContract{KernelServices: []string{"kernel.info"}},
	}
	_, err := transaction.startPlugin(context.Background(), plugin)
	if err == nil || !strings.Contains(err.Error(), "Platform Profile") {
		t.Fatalf("缺失服务级 Grant 必须在进入执行驱动前拒绝: %v", err)
	}
}
