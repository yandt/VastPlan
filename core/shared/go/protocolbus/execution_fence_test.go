package protocolbus

import (
	"context"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/operationfence"
)

type testFenceProvider struct {
	evidence operationfence.Evidence
	current  bool
}

func (p *testFenceProvider) Current() (operationfence.Evidence, bool) { return p.evidence, p.current }

func TestHostServiceReceivesOnlyCurrentExecutionFence(t *testing.T) {
	reg := registry.New()
	reg.DefinePoint(registry.ExtensionPoint{Name: extpoint.KernelService, Dispatch: registry.DispatchSingle})
	host := NewHost("backend", "1.0.0", reg, nil)
	provider := &testFenceProvider{evidence: operationfence.Evidence{LogicalService: "platform.deployment", UnitID: "deployment", Epoch: 9, Token: "token-9"}, current: true}
	host.SetExecutionFenceProvider(provider)
	observed := false
	if err := host.RegisterHostService(extpoint.KernelService, "kernel.test.fenced", func(ctx context.Context, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
		_, observed = operationfence.FromContext(ctx)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	target := &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: "kernel.test.fenced"}
	if _, err := host.callHostService(context.Background(), target, &contractv1.CallContext{}, nil); err != nil || !observed {
		t.Fatalf("当前 leader evidence 未注入 HostService: observed=%v err=%v", observed, err)
	}
	provider.current = false
	observed = true
	if _, err := host.callHostService(context.Background(), target, &contractv1.CallContext{}, nil); err != nil || observed {
		t.Fatalf("失效 evidence 不得继续注入: observed=%v err=%v", observed, err)
	}
}

func TestExternalHostCallsStopImmediatelyAfterLeaderLeaseLoss(t *testing.T) {
	host := NewHost("backend", "1.0.0", registry.New(), nil)
	if !host.externalHostCallAllowed() {
		t.Fatal("非 leader runtime 不应被额外拦截")
	}
	provider := &testFenceProvider{evidence: operationfence.Evidence{LogicalService: "platform.assessment", UnitID: "controller", Epoch: 2, Token: "token-2"}, current: true}
	host.SetExecutionFenceProvider(provider)
	if !host.externalHostCallAllowed() {
		t.Fatal("当前 leader 应允许外部能力调用")
	}
	provider.current = false
	if host.externalHostCallAllowed() {
		t.Fatal("失去 leader lease 后必须在 Runtime Host 阻止外部能力调用")
	}
}

func TestLocalPluginHostCallDoesNotRequireExternalExecutionFence(t *testing.T) {
	reg := registry.New()
	reg.DefinePoint(registry.ExtensionPoint{Name: extpoint.AuthenticationProvider, Dispatch: registry.DispatchSingle})
	if err := reg.Register(registry.Contribution{ExtensionPoint: extpoint.AuthenticationProvider, ID: "seed-local", PluginID: "cn.vastplan.seed"}); err != nil {
		t.Fatal(err)
	}
	host := NewHost("backend", "1.0.0", reg, nil)
	host.byPlugin["cn.vastplan.seed"] = &session{}
	host.SetExecutionFenceProvider(&testFenceProvider{current: false})
	local := &contractv1.CallTarget{ExtensionPoint: extpoint.AuthenticationProvider, Capability: "seed-local"}
	if !host.localHostCall(local) {
		t.Fatal("同一 Runtime Host 的插件贡献必须识别为本地调用")
	}
	remote := &contractv1.CallTarget{ExtensionPoint: extpoint.AuthenticationProvider, Capability: "remote-provider"}
	if host.localHostCall(remote) {
		t.Fatal("未在本 Host 注册的能力不得绕过外部 execution fence")
	}
}
