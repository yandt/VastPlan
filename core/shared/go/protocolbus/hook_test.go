package protocolbus

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
)

// newHookTestHost 构造不经网络的宿主：Hook 的管道语义在这里做快速单元验证，
// 跨进程注册与真实插件执行则由 engineering/e2e/dispatch_test.go 覆盖。
func newHookTestHost(t *testing.T) *Host {
	t.Helper()
	reg := registry.New()
	for _, point := range []registry.ExtensionPoint{
		{Name: extpoint.ToolPackage, Dispatch: registry.DispatchSingle},
		{Name: extpoint.PermissionChecker, Dispatch: registry.DispatchSelect},
		{Name: extpoint.Hook, Dispatch: registry.DispatchFanout},
	} {
		reg.DefinePoint(point)
	}
	return NewHost("backend", "0.1.0", reg, func(string, ...any) {})
}

func registerAllowAll(t *testing.T, h *Host) {
	t.Helper()
	err := h.RegisterHostService(extpoint.PermissionChecker, "test.allow-all",
		func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK},
				[]byte(`{"decision":"allow"}`), nil
		})
	if err != nil {
		t.Fatalf("注册测试权限策略失败: %v", err)
	}
}

func registerHook(t *testing.T, h *Host, id string, priority int, phase extpoint.Phase, fn HostService) {
	t.Helper()
	if err := h.RegisterHostService(extpoint.Hook, id, fn); err != nil {
		t.Fatalf("注册 Hook 服务 %s 失败: %v", id, err)
	}
	descriptor, err := json.Marshal(extpoint.HookDescriptor{Point: extpoint.PointInvoke, Phase: phase})
	if err != nil {
		t.Fatalf("序列化 Hook descriptor 失败: %v", err)
	}
	// RegisterHostService 先登记无 descriptor 的能力；同 id 的 fanout 贡献在此补齐 descriptor 与 priority。
	if err := h.Registry.Register(registry.Contribution{
		ExtensionPoint: extpoint.Hook,
		ID:             id,
		PluginID:       KernelPluginID,
		Priority:       priority,
		Descriptor:     descriptor,
	}); err != nil {
		t.Fatalf("登记 Hook descriptor %s 失败: %v", id, err)
	}
}

func registerTool(t *testing.T, h *Host, fn HostService) {
	t.Helper()
	if err := h.RegisterHostService(extpoint.ToolPackage, "test.tool", fn); err != nil {
		t.Fatalf("注册测试工具失败: %v", err)
	}
}

func hookResult(t *testing.T, response extpoint.HookResponse) ([]byte, *contractv1.CallResult) {
	t.Helper()
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("序列化 Hook 回答失败: %v", err)
	}
	return payload, &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}
}

func testHookTarget() *contractv1.CallTarget {
	op := "run"
	return &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: "test.tool", Operation: &op}
}

// before 按 priority 顺序执行；任一钩子否决后，不得继续进入权限/目标/after 阶段。
func TestHostInvoke_BeforeHooksAreOrderedAndAbort(t *testing.T) {
	h := newHookTestHost(t)
	var order []string
	registerHook(t, h, "hook.high", 100, extpoint.PhaseBefore,
		func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			order = append(order, "high")
			payload, result := hookResult(t, extpoint.HookResponse{})
			return result, payload, nil
		})
	registerHook(t, h, "hook.abort", 10, extpoint.PhaseBefore,
		func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			order = append(order, "abort")
			payload, result := hookResult(t, extpoint.HookResponse{Abort: true, Reason: "配额已耗尽"})
			return result, payload, nil
		})
	registerTool(t, h,
		func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			order = append(order, "target")
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, nil, nil
		})

	resp, err := h.Invoke(context.Background(), testHookTarget(), &contractv1.CallContext{}, nil)
	if err != nil {
		t.Fatalf("钩子否决应是应用层结果: %v", err)
	}
	if resp.Result.GetError().GetCode() != "hook.aborted" {
		t.Fatalf("否决错误码 = %q，期望 hook.aborted", resp.Result.GetError().GetCode())
	}
	if want := []string{"high", "abort"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("before 执行顺序 = %v，期望 %v", order, want)
	}
}

// after 看到已完成的结果，但即使它回 Abort，也不能改写主调用的成功结论。
func TestHostInvoke_AfterHookOnlyObservesResult(t *testing.T) {
	h := newHookTestHost(t)
	registerAllowAll(t, h)
	var observed extpoint.HookRequest
	registerHook(t, h, "hook.observer", 10, extpoint.PhaseAfter,
		func(_ context.Context, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if err := json.Unmarshal(payload, &observed); err != nil {
				return nil, nil, err
			}
			out, result := hookResult(t, extpoint.HookResponse{Abort: true, Reason: "after 不能改写结论"})
			return result, out, nil
		})
	registerTool(t, h,
		func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, []byte(`"target-ok"`), nil
		})

	resp, err := h.Invoke(context.Background(), testHookTarget(), &contractv1.CallContext{}, nil)
	if err != nil {
		t.Fatalf("主调用失败: %v", err)
	}
	if resp.Result.Status != contractv1.CallResult_STATUS_OK || string(resp.Payload) != `"target-ok"` {
		t.Fatalf("after 钩子不应改写调用结论，实际 result=%+v payload=%s", resp.Result, resp.Payload)
	}
	if observed.Point != extpoint.PointInvoke || observed.Phase != extpoint.PhaseAfter ||
		observed.Target.Capability != "test.tool" || observed.Result == nil || observed.Result.Status != "STATUS_OK" {
		t.Fatalf("after 钩子未收到完整观察上下文: %+v", observed)
	}
}

// Hook 是横切增强：before 钩子不可达时按当前 fail-open 约定记录并继续主调用。
func TestHostInvoke_BeforeHookFailureFailsOpen(t *testing.T) {
	h := newHookTestHost(t)
	registerAllowAll(t, h)
	registerHook(t, h, "hook.unavailable", 10, extpoint.PhaseBefore,
		func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			return nil, nil, errors.New("模拟 Hook 不可达")
		})
	registerTool(t, h,
		func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, nil, nil
		})

	resp, err := h.Invoke(context.Background(), testHookTarget(), &contractv1.CallContext{}, nil)
	if err != nil || resp.Result.Status != contractv1.CallResult_STATUS_OK {
		t.Fatalf("before Hook 失败应 fail-open，实际 resp=%+v err=%v", resp, err)
	}
}
