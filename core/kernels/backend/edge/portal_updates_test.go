package edge

import (
	"testing"
	"time"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func TestPortalUpdateHubScopesAndCoalescesByPortal(t *testing.T) {
	hub := NewPortalUpdateHub()
	events, cancel := hub.subscribe("tenant-a", "operations")
	defer cancel()
	hub.Publish(PortalUpdate{TenantID: "tenant-b", PortalID: "operations", Activation: 2, Mode: "generation"})
	hub.Publish(PortalUpdate{TenantID: "tenant-a", PortalID: "other", Activation: 2, Mode: "generation"})
	hub.Publish(PortalUpdate{TenantID: "tenant-a", PortalID: "operations", Activation: 2, Mode: "generation"})
	hub.Publish(PortalUpdate{TenantID: "tenant-a", PortalID: "operations", Activation: 3, Mode: "generation"})
	select {
	case update := <-events:
		if update.Activation != 3 || update.TenantID != "tenant-a" || update.PortalID != "operations" {
			t.Fatalf("Portal update 未按租户/门户合并: %+v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到 Portal update")
	}
}

func TestClassifyPortalUpdateUsesHostEpochForRendererBoundary(t *testing.T) {
	base := portalapi.PortalSpec{Revision: 1, RenderAdapter: portalapi.RenderAdapter{PluginRef: portalapi.PluginRef{ID: "adapter", Version: "1.0.0"}, UIContract: "^4.0.0", Config: frontendcompositionv1.RenderAdapterConfig{DefaultRenderer: "arco"}}, Shell: portalapi.Shell{UIContract: "^4.0.0"}, Workbench: portalapi.Workbench{UIContract: "^4.0.0"}}
	compatible := base
	compatible.Revision = 2
	if got := classifyPortalUpdate(base, compatible); got != "generation" {
		t.Fatalf("功能变更应使用 Portal Generation: %s", got)
	}
	host := compatible
	host.RenderAdapter.Config.DefaultRenderer = "mui"
	if got := classifyPortalUpdate(base, host); got != "host-epoch" {
		t.Fatalf("默认 Renderer 切换应使用 Host Epoch: %s", got)
	}
}
