package protocolbus

import (
	"context"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
)

func TestCapabilityGrantPlanIsExactAndImmutable(t *testing.T) {
	requested := []string{"kernel.config.get", "kernel.state.shared.get"}
	plan, err := compileCapabilityGrantPlan(requested)
	if err != nil {
		t.Fatal(err)
	}
	requested[0] = "kernel.evil"
	if !plan.allowsKernelService("kernel.config.get") || plan.allowsKernelService("kernel.evil") || plan.allowsKernelService("") {
		t.Fatalf("Grant Plan 必须只包含编译时精确能力: %v", plan.KernelServices())
	}
	if _, err := compileCapabilityGrantPlan([]string{"kernel.config.get", "kernel.config.get"}); err == nil {
		t.Fatal("重复 grant 必须 fail-closed")
	}
}

func TestCapabilityGrantPlanRequiresRegisteredHostService(t *testing.T) {
	reg := registry.New()
	reg.DefinePoint(registry.ExtensionPoint{Name: "kernel.service", Dispatch: registry.DispatchSingle})
	host := NewHost("backend", "1.0.0", reg, nil)
	if err := host.validateKernelServiceGrants([]string{"kernel.missing"}); err == nil {
		t.Fatal("未注册的 Kernel Service Grant 必须在启动前拒绝")
	}
	if err := host.RegisterHostService("kernel.service", "kernel.available", func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := host.validateKernelServiceGrants([]string{"kernel.available"}); err != nil {
		t.Fatalf("已注册服务应通过 Grant 校验: %v", err)
	}
}
