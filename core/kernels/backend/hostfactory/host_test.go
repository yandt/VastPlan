package hostfactory

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
)

func TestNew_DefinesClosedBackendCatalogAndInternalService(t *testing.T) {
	host, err := New("1.0.0", t.Logf)
	if err != nil {
		t.Fatalf("创建 Backend 宿主失败: %v", err)
	}

	got := make([]string, 0)
	for _, point := range host.Registry.Points() {
		got = append(got, point.Name)
	}
	want := append(extpoint.BackendPluginPoints(), extpoint.KernelService)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Backend Registry 与封板目录漂移: got=%v want=%v", got, want)
	}

	service, ok := host.Registry.Lookup(extpoint.KernelService, "kernel.info")
	if !ok || service.PluginID != "__kernel__" {
		t.Fatalf("kernel.info 必须仅由内核登记: %+v ok=%v", service, ok)
	}
	diagnostics, ok := host.Registry.Lookup(extpoint.KernelService, "kernel.diagnostics")
	if !ok || diagnostics.PluginID != "__kernel__" {
		t.Fatalf("kernel.diagnostics 必须仅由内核登记: %+v ok=%v", diagnostics, ok)
	}
}

func TestKernelConfigGetRequiresAuthenticatedPluginAndReturnsFrozenValue(t *testing.T) {
	provider, err := kernelspi.NewMapConfig(map[string]any{"retries": 3})
	if err != nil {
		t.Fatal(err)
	}
	service := kernelConfigGet(provider)
	pluginCtx := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "plugin.a"}}
	result, payload, err := service(context.Background(), pluginCtx, []byte(`{"key":"retries"}`))
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("读取配置失败: %v %+v", err, result)
	}
	var retries int
	if err := json.Unmarshal(payload, &retries); err != nil || retries != 3 {
		t.Fatalf("配置值错误: %s", payload)
	}
	if _, _, err := service(context.Background(), &contractv1.CallContext{}, []byte(`{"key":"retries"}`)); err == nil {
		t.Fatal("非插件调用必须 fail-closed")
	}
}
