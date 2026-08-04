package frontendcompositionv1

import (
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestCompilePortalBrowserExposureUsesPluginDeclarationsAndPlatformClosures(t *testing.T) {
	profile := validProfile(t)
	profile.Plugins = append(profile.Plugins, PluginRef{ID: "cn.vastplan.test.management", Version: "1.0.0", Channel: "stable"})
	catalog := PortalPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "catalog"},
		Profiles: []PlatformProfile{profile},
		Bindings: []PortalBinding{{
			TenantID: "tenant-a", PortalID: "operations",
			PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
			Services:        []ManagedService{{ID: "settings", LogicalService: "platform.settings", RoutingDomain: "platform", Capabilities: []CapabilityGrant{{Capability: "platform.settings"}}}},
		}},
		BrowserExposure: &BrowserExposurePolicy{DisabledOperations: []BrowserExposureOperationDisable{{PluginID: "cn.vastplan.test.management", Capability: "platform.settings", Operation: "put"}}},
	}
	manifest := browserExposureManifest(t)
	compiled, err := CompilePortalBrowserExposure(catalog, func(ref PluginRef) (pluginv1.Manifest, error) {
		if ref.ID == manifest.ID {
			return manifest, nil
		}
		return pluginv1.Manifest{ID: ref.ID, Version: ref.Version}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	grant := compiled.Bindings[0].Services[0].Capabilities[0]
	if len(grant.Read) != 1 || grant.Read[0] != "list" || len(grant.Write) != 0 {
		t.Fatalf("浏览器操作应只保留插件声明且未被禁用的 list: %+v", grant)
	}
	if _, err := ValidateResolvedPortalPlatformCatalog(compiled); err != nil {
		t.Fatalf("已编译目录必须可用于 Portal 运行: %v", err)
	}
}

func TestCompilePortalBrowserExposureRejectsManualOperationGrantsAndUnknownClosure(t *testing.T) {
	profile := validProfile(t)
	profile.Plugins = append(profile.Plugins, PluginRef{ID: "cn.vastplan.test.management", Version: "1.0.0", Channel: "stable"})
	base := PortalPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "catalog"},
		Profiles: []PlatformProfile{profile},
		Bindings: []PortalBinding{{
			TenantID: "tenant-a", PortalID: "operations",
			PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
			Services:        []ManagedService{{ID: "settings", LogicalService: "platform.settings", RoutingDomain: "platform", Capabilities: []CapabilityGrant{{Capability: "platform.settings", Read: []string{"list"}}}}},
		}},
	}
	manifest := browserExposureManifest(t)
	resolver := func(ref PluginRef) (pluginv1.Manifest, error) {
		if ref.ID == manifest.ID {
			return manifest, nil
		}
		return pluginv1.Manifest{ID: ref.ID, Version: ref.Version}, nil
	}
	if _, err := CompilePortalBrowserExposure(base, resolver); err == nil {
		t.Fatal("源目录手工声明 operation grant 必须拒绝")
	}
	base.Bindings[0].Services[0].Capabilities[0].Read = nil
	base.BrowserExposure = &BrowserExposurePolicy{DisabledOperations: []BrowserExposureOperationDisable{{PluginID: "cn.vastplan.test.management", Capability: "platform.settings", Operation: "missing"}}}
	if _, err := CompilePortalBrowserExposure(base, resolver); err == nil {
		t.Fatal("平台只能关闭插件已声明的浏览器操作")
	}
}

func browserExposureManifest(t *testing.T) pluginv1.Manifest {
	t.Helper()
	manifest, err := pluginv1.ParseManifest([]byte(`{"id":"cn.vastplan.test.management","name":"management","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},"authorization":{"namespace":"cn.vastplan.test.management","capabilities":["platform.settings"],"browserExposed":true,"permissions":[{"code":"cn.vastplan.test.management.read","title":"read","scope":"tenant","risk":"low","assignable":true,"offlineAllowed":false},{"code":"cn.vastplan.test.management.write","title":"write","scope":"tenant","risk":"low","assignable":true,"offlineAllowed":false}],"operationGuards":[{"extensionPoint":"tool.package","capability":"platform.settings","operation":"list","permissions":["cn.vastplan.test.management.read"],"access":"read","approval":"none"},{"extensionPoint":"tool.package","capability":"platform.settings","operation":"put","permissions":["cn.vastplan.test.management.write"],"access":"write","approval":"none"}]},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[{"id":"platform.settings","service_role":"backend","subcommands":[{"name":"list","description":"list"},{"name":"put","description":"put"}]}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
