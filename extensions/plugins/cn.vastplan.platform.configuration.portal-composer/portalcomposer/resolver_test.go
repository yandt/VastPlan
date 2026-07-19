package portalcomposer

import (
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
)

func TestResolveInjectsPlatformRenderAdapterAndLocksInputs(t *testing.T) {
	profile := testProfile()
	app := testComposition("/")
	resolved, err := resolve(testPlatformCatalog(), app, "tenant-a", 7)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Revision != 7 || len(resolved.Plugins) != 5 || resolved.RenderAdapter.ID != profile.RenderAdapter.ID || resolved.StructureComposition.ID != profile.StructureComposition.ID || resolved.StructureLayout.ID != profile.StructureLayout.ID || resolved.Workbench.ID != profile.Workbench.ID {
		t.Fatalf("解析结果错误: %+v", resolved)
	}
	if resolved.Resolution.PluginOrigins[profile.RenderAdapter.ID] != compositioncommonv1.OriginPlatformProfile || resolved.Resolution.PluginOrigins[app.Plugins[0].ID] != compositioncommonv1.OriginApplication {
		t.Fatalf("来源锁错误: %+v", resolved.Resolution)
	}
	if len(resolved.Resolution.PlatformCatalog.Digest) != 64 || len(resolved.Resolution.PlatformProfile.Digest) != 64 || len(resolved.Resolution.ApplicationComposition.Digest) != 64 || len(resolved.Resolution.ManagementBindingDigest) != 64 {
		t.Fatal("输入摘要未锁定")
	}
	if resolved.Management.TenantID != "tenant-a" || resolved.Management.Services[0].LogicalService != "platform.settings" {
		t.Fatalf("管理绑定未锁定: %+v", resolved.Management)
	}
	if len(resolved.StructureComposition.Config.NavigationGroups) == 0 {
		t.Fatal("Shell 组合配置未透传到浏览器运行描述")
	}
}

func TestResolveRejectsApplicationOverride(t *testing.T) {
	profile := testProfile()
	app := testComposition("/")
	app.Plugins[0] = profile.Plugins[0]
	if _, err := resolve(testPlatformCatalog(), app, "tenant-a", 1); err == nil {
		t.Fatal("应用覆盖平台插件必须拒绝")
	}
}

func testPlatformCatalog() frontendcompositionv1.PortalPlatformCatalog {
	profile := testProfile()
	return frontendcompositionv1.PortalPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "portal-platform"},
		Profiles: []frontendcompositionv1.PlatformProfile{profile},
		Bindings: []frontendcompositionv1.PortalBinding{{
			TenantID: "tenant-a", PortalID: "admin",
			PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
			Services:        []frontendcompositionv1.ManagedService{{ID: "settings", LogicalService: "platform.settings", RoutingDomain: "platform", Capabilities: []frontendcompositionv1.CapabilityGrant{{Capability: "platform.settings", Read: []string{"list"}, Write: []string{"put", "delete"}}}}},
		}},
	}
}

func testProfile() frontendcompositionv1.PlatformProfile {
	design := frontendcompositionv1.PluginRef{ID: "cn.vastplan.foundation.frontend.render.adapter.arco", Version: "1.0.0", Channel: "stable"}
	composition := frontendcompositionv1.PluginRef{ID: "cn.vastplan.foundation.frontend.structure.composition.standard", Version: "1.0.0", Channel: "stable"}
	layout := frontendcompositionv1.PluginRef{ID: "cn.vastplan.foundation.frontend.structure.layout.standard", Version: "1.0.0", Channel: "stable"}
	workbench := frontendcompositionv1.PluginRef{ID: "cn.vastplan.foundation.frontend.workflow.workbench", Version: "1.0.0", Channel: "stable"}
	return frontendcompositionv1.PlatformProfile{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "portal-default"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend}, RenderAdapter: frontendcompositionv1.RenderAdapter{PluginRef: design, UIContract: "^3.0.0"}, StructureComposition: frontendcompositionv1.StructureComposition{PluginRef: composition, UIContract: "^3.0.0", Config: frontendcompositionv1.NavigationConfig{NavigationGroups: []frontendcompositionv1.NavigationGroupDescriptor{{ID: "operations", Label: "运行管理", Zone: "primary", Icon: "menu"}}}}, StructureLayout: frontendcompositionv1.StructureLayout{PluginRef: layout, UIContract: "^3.0.0"}, Workbench: frontendcompositionv1.Workbench{PluginRef: workbench, UIContract: "^3.0.0"}, Plugins: []frontendcompositionv1.PluginRef{design, composition, layout, workbench}, Security: frontendcompositionv1.SecurityPolicy{FirstPartyOnly: true, RequireIntegrity: true}}
}

func testComposition(route string) frontendcompositionv1.ApplicationComposition {
	return frontendcompositionv1.ApplicationComposition{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "admin"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend}, Route: route, Plugins: []frontendcompositionv1.PluginRef{{ID: "cn.vastplan.product.frontend.admin", Version: "1.0.0", Channel: "stable"}}}
}
