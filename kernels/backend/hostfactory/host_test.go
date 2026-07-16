package hostfactory

import (
	"reflect"
	"sort"
	"testing"

	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
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
