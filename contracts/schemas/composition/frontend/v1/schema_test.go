package frontendcompositionv1

import (
	"fmt"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func TestFrontendInputsSeparatePlatformAndApplication(t *testing.T) {
	profile, err := ParsePlatformProfile([]byte(validShellProfileJSON("portal-default", 2, `{"navigationGroups":[{"id":"operations","label":"运行管理","zone":"primary","icon":"menu"}],"defaultTemplate":"standard","allowedTemplates":["standard","top-navigation"],"userSelectable":true}`)))
	if err != nil || profile.Plugins[0].Channel != "stable" || len(profile.Shell.Config.NavigationGroups) != 1 || len(profile.Digest()) != 64 {
		t.Fatalf("profile 无效: %+v %v", profile, err)
	}
	app, err := ParseApplicationComposition([]byte(`{"version":1,"revision":3,"id":"operations","target":{"kernel":"frontend"},"route":"/operations","plugins":[{"id":"cn.vastplan.product.frontend.operations","version":"1.0.0"},{"id":"cn.vastplan.foundation.frontend.workflow.workbench","version":"1.0.0"}]}`))
	if err != nil || app.Plugins[0].Channel != "stable" || len(app.Digest()) != 64 {
		t.Fatalf("application 无效: %+v %v", app, err)
	}
}

func TestFrontendInputsRejectBoundaryViolations(t *testing.T) {
	if _, err := ParsePlatformProfile([]byte(`{"version":1,"revision":1,"id":"x","target":{"kernel":"frontend"},"renderAdapter":{"id":"cn.vastplan.foundation.frontend.render.adapter.arco","version":"1.0.0","uiContract":"^2"},"plugins":[,{"id":"cn.vastplan.foundation.frontend.workflow.workbench","version":"1.0.0"}]}`)); err == nil {
		t.Fatal("平台 plugins 缺设计系统必须拒绝")
	}
	if _, err := ParseApplicationComposition([]byte(`{"version":1,"revision":1,"id":"x","target":{"kernel":"frontend"},"route":"/","renderAdapter":{},"plugins":[,{"id":"cn.vastplan.foundation.frontend.workflow.workbench","version":"1.0.0"}]}`)); err == nil {
		t.Fatal("应用输入不得携带 renderAdapter")
	}
}

func TestPlatformProfileUsesThemeTemplateSelection(t *testing.T) {
	base := `{"version":1,"revision":1,"id":"theme-template","target":{"kernel":"frontend"},"renderAdapter":{"id":"cn.vastplan.foundation.frontend.render.adapter.arco","version":"1.0.0","uiContract":"^4.0.0","config":%s},"shell":{"id":"cn.vastplan.foundation.frontend.structure.shell","version":"1.0.0","uiContract":"^4.0.0","config":{"defaultTemplate":"standard","allowedTemplates":["standard"],"userSelectable":false}},"workbench":{"id":"cn.vastplan.foundation.frontend.workflow.workbench","version":"1.0.0","uiContract":"^4.0.0"},"plugins":[{"id":"cn.vastplan.foundation.frontend.render.adapter.arco","version":"1.0.0"},{"id":"cn.vastplan.foundation.frontend.structure.shell","version":"1.0.0"},{"id":"cn.vastplan.foundation.frontend.workflow.workbench","version":"1.0.0"}],"security":{"firstPartyOnly":true,"requireIntegrity":true}}`
	profile, err := ParsePlatformProfile([]byte(fmt.Sprintf(base, `{"themeTemplate":"dark"}`)))
	if err != nil || profile.RenderAdapter.Config["themeTemplate"] != "dark" {
		t.Fatalf("主题模板选择应能解析: %+v %v", profile.RenderAdapter.Config, err)
	}
	if _, err := ParsePlatformProfile([]byte(fmt.Sprintf(base, `{"theme":"dark"}`))); err == nil {
		t.Fatal("旧 theme 字段必须拒绝，避免与通用 themeTemplate 契约并存")
	}
}

func TestPlatformProfileValidatesBoundedNavigationTree(t *testing.T) {
	valid := validShellProfileJSON("tree", 1, `{"navigationGroups":[{"id":"operations","label":"运行管理","zone":"primary","icon":"menu"},{"id":"deployments","parentID":"operations","label":"部署","zone":"primary","icon":"menu"}],"defaultTemplate":"standard","allowedTemplates":["standard"],"userSelectable":false}`)
	if _, err := ParsePlatformProfile([]byte(valid)); err != nil {
		t.Fatalf("两级导航组应有效: %v", err)
	}

	crossZone := validShellProfileJSON("tree", 1, `{"navigationGroups":[{"id":"operations","label":"运行管理","zone":"primary","icon":"menu"},{"id":"settings","parentID":"operations","label":"设置","zone":"settings","icon":"settings"}],"defaultTemplate":"standard","allowedTemplates":["standard"],"userSelectable":false}`)
	if _, err := ParsePlatformProfile([]byte(crossZone)); err == nil {
		t.Fatal("子导航组不得跨 zone 继承")
	}
}

func TestFrontendLocalizationPolicyRequiresGovernedDefault(t *testing.T) {
	profile, err := ParsePlatformProfile([]byte(validShellProfileJSON("localized", 1, `{"defaultTemplate":"standard","allowedTemplates":["standard"],"userSelectable":false}`, `{"defaultLocale":"en-US","supportedLocales":["zh-CN","en-US"]}`)))
	if err != nil || profile.Localization == nil || profile.Localization.DefaultLocale != "en-US" {
		t.Fatalf("本地化策略解析失败: %+v %v", profile.Localization, err)
	}
	if _, err := ParsePlatformProfile([]byte(validShellProfileJSON("localized", 1, `{"defaultTemplate":"standard","allowedTemplates":["standard"],"userSelectable":false}`, `{"defaultLocale":"fr-FR","supportedLocales":["zh-CN","en-US"]}`))); err == nil {
		t.Fatal("默认语言不在 supportedLocales 中必须拒绝")
	}
}

func TestPortalPlatformCatalogResolvesProfileAndExactServiceGrants(t *testing.T) {
	profile := validProfile(t)
	catalog, err := ValidatePortalPlatformCatalog(PortalPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 3, ID: "enterprise-portals"},
		Profiles: []PlatformProfile{profile},
		Bindings: []PortalBinding{{
			TenantID: "tenant-a", PortalID: "operations",
			PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
			Services: []ManagedService{{
				ID: "settings-primary", Label: "全局设置", LogicalService: "platform.settings.primary", RoutingDomain: "platform",
				Capabilities: []CapabilityGrant{{Capability: "platform.settings", Read: []string{"list"}, Write: []string{"put", "delete"}}},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved, binding, err := catalog.Resolve("tenant-a", "operations")
	if err != nil || resolved.ID != profile.ID || binding.Services[0].LogicalService != "platform.settings.primary" {
		t.Fatalf("解析绑定失败: profile=%+v binding=%+v err=%v", resolved, binding, err)
	}
	if _, _, err := catalog.Resolve("tenant-b", "operations"); err == nil {
		t.Fatal("不同租户不得共享 Portal 管理绑定")
	}
}

func TestPortalPlatformCatalogRejectsWideningAndStaleProfileLocks(t *testing.T) {
	profile := validProfile(t)
	base := PortalPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "enterprise-portals"},
		Profiles: []PlatformProfile{profile},
		Bindings: []PortalBinding{{TenantID: "tenant-a", PortalID: "operations", PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}, Services: []ManagedService{{ID: "settings", LogicalService: "platform.settings", RoutingDomain: "platform", Capabilities: []CapabilityGrant{{Capability: "platform.settings", Read: []string{"list"}, Write: []string{"list"}}}}}}},
	}
	if _, err := ValidatePortalPlatformCatalog(base); err == nil {
		t.Fatal("同一 operation 不得同时出现在 read 与 write")
	}
	base.Bindings[0].Services[0].Capabilities[0].Write = []string{"put"}
	base.Bindings[0].PlatformProfile.Digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := ValidatePortalPlatformCatalog(base); err == nil {
		t.Fatal("过期 Platform Profile 摘要锁必须拒绝")
	}
}

func TestPortalPlatformCatalogAllowsExplicitCrossPortalServiceOverlap(t *testing.T) {
	primary := validProfile(t)
	compact := primary
	compact.ID = "portal-compact"
	service := ManagedService{ID: "settings", LogicalService: "platform.settings", RoutingDomain: "platform", Capabilities: []CapabilityGrant{{Capability: "platform.settings", Read: []string{"list"}}}}
	catalog, err := ValidatePortalPlatformCatalog(PortalPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "enterprise-portals"},
		Profiles: []PlatformProfile{primary, compact},
		Bindings: []PortalBinding{
			{TenantID: "tenant-a", PortalID: "operations", PlatformProfile: compositioncommonv1.Ref{ID: primary.ID, Revision: primary.Revision, Digest: primary.Digest()}, Services: []ManagedService{service}},
			{TenantID: "tenant-a", PortalID: "security", PlatformProfile: compositioncommonv1.Ref{ID: compact.ID, Revision: compact.Revision, Digest: compact.Digest()}, Services: []ManagedService{service}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, _, err := catalog.Resolve("tenant-a", "security")
	if err != nil || profile.ID != "portal-compact" {
		t.Fatalf("多 Portal 独立 Profile 解析失败: %+v %v", profile, err)
	}
}

func validProfile(t *testing.T) PlatformProfile {
	t.Helper()
	profile, err := ParsePlatformProfile([]byte(validShellProfileJSON("portal-default", 2, `{"defaultTemplate":"standard","allowedTemplates":["standard"],"userSelectable":false}`)))
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func validShellProfileJSON(id string, revision int, shellConfig string, localization ...string) string {
	locale := ""
	if len(localization) > 0 {
		locale = `,"localization":` + localization[0]
	}
	return fmt.Sprintf(`{"version":1,"revision":%d,"id":"%s","target":{"kernel":"frontend"},"renderAdapter":{"id":"cn.vastplan.foundation.frontend.render.adapter.arco","version":"1.0.0","uiContract":"^4.0.0"},"shell":{"id":"cn.vastplan.foundation.frontend.structure.shell","version":"1.0.0","uiContract":"^4.0.0","config":%s},"workbench":{"id":"cn.vastplan.foundation.frontend.workflow.workbench","version":"1.0.0","uiContract":"^4.0.0"}%s,"plugins":[{"id":"cn.vastplan.foundation.frontend.render.adapter.arco","version":"1.0.0"},{"id":"cn.vastplan.foundation.frontend.structure.shell","version":"1.0.0"},{"id":"cn.vastplan.foundation.frontend.workflow.workbench","version":"1.0.0"}],"security":{"firstPartyOnly":true,"requireIntegrity":true}}`, revision, id, shellConfig, locale)
}
