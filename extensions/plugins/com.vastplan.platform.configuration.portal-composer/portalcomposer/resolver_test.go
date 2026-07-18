package portalcomposer

import (
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
)

func TestResolveInjectsPlatformDesignSystemAndLocksInputs(t *testing.T) {
	profile := testProfile()
	app := testComposition("/")
	resolved, err := resolve(profile, app, "tenant-a", 7)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Revision != 7 || len(resolved.Plugins) != 4 || resolved.DesignSystem.ID != profile.DesignSystem.ID || resolved.Composition.ID != profile.Composition.ID || resolved.Layout.ID != profile.Layout.ID {
		t.Fatalf("解析结果错误: %+v", resolved)
	}
	if resolved.Resolution.PluginOrigins[profile.DesignSystem.ID] != compositioncommonv1.OriginPlatformProfile || resolved.Resolution.PluginOrigins[app.Plugins[0].ID] != compositioncommonv1.OriginApplication {
		t.Fatalf("来源锁错误: %+v", resolved.Resolution)
	}
	if len(resolved.Resolution.PlatformProfile.Digest) != 64 || len(resolved.Resolution.ApplicationComposition.Digest) != 64 {
		t.Fatal("输入摘要未锁定")
	}
}

func TestResolveRejectsApplicationOverride(t *testing.T) {
	profile := testProfile()
	app := testComposition("/")
	app.Plugins[0] = profile.Plugins[0]
	if _, err := resolve(profile, app, "tenant-a", 1); err == nil {
		t.Fatal("应用覆盖平台插件必须拒绝")
	}
}

func testProfile() frontendcompositionv1.PlatformProfile {
	design := frontendcompositionv1.PluginRef{ID: "com.vastplan.foundation.frontend.design-system.arco", Version: "1.0.0", Channel: "stable"}
	composition := frontendcompositionv1.PluginRef{ID: "com.vastplan.foundation.frontend.composition.standard", Version: "1.0.0", Channel: "stable"}
	layout := frontendcompositionv1.PluginRef{ID: "com.vastplan.foundation.frontend.layout.standard", Version: "1.0.0", Channel: "stable"}
	return frontendcompositionv1.PlatformProfile{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "portal-default"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend}, DesignSystem: frontendcompositionv1.DesignSystem{PluginRef: design, UIContract: "^1.0.0"}, Composition: frontendcompositionv1.ShellComposition{PluginRef: composition, UIContract: "^1.0.0"}, Layout: frontendcompositionv1.ShellLayout{PluginRef: layout, UIContract: "^1.0.0"}, Plugins: []frontendcompositionv1.PluginRef{design, composition, layout}, Security: frontendcompositionv1.SecurityPolicy{FirstPartyOnly: true, RequireIntegrity: true}}
}

func testComposition(route string) frontendcompositionv1.ApplicationComposition {
	return frontendcompositionv1.ApplicationComposition{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "admin"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend}, Route: route, Plugins: []frontendcompositionv1.PluginRef{{ID: "com.vastplan.product.frontend.admin", Version: "1.0.0", Channel: "stable"}}}
}
