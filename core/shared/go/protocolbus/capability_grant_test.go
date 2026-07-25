package protocolbus

import "testing"

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
