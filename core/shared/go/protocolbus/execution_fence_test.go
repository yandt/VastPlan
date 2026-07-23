package protocolbus

import (
	"context"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/operationfence"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
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
