package portaltrust

import (
	"encoding/json"
	"strings"
	"testing"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func TestNavigationCandidateRejectsUnknownAndCyclicOverrides(t *testing.T) {
	index := navigationIndexFixture(t)
	unknown := portalapi.PortalSpec{Shell: portalapi.Shell{Config: frontendcompositionv1.ShellConfig{NavigationConfig: frontendcompositionv1.NavigationConfig{NavigationOverrides: []frontendcompositionv1.NavigationOverride{{Target: "cn.vastplan.missing/root"}}}}}}
	if err := validateNavigationCandidates(unknown, index); err == nil || !strings.Contains(err.Error(), "未安装节点") {
		t.Fatalf("应拒绝未知覆盖，得到: %v", err)
	}
	cyclic := portalapi.PortalSpec{Shell: portalapi.Shell{Config: frontendcompositionv1.ShellConfig{NavigationConfig: frontendcompositionv1.NavigationConfig{NavigationOverrides: []frontendcompositionv1.NavigationOverride{{Target: "cn.vastplan.test.navigation/root", Parent: "cn.vastplan.test.navigation/child"}}}}}}
	if err := validateNavigationCandidates(cyclic, index); err == nil || !strings.Contains(err.Error(), "循环或深度") {
		t.Fatalf("应拒绝循环覆盖，得到: %v", err)
	}
}

func TestNavigationCandidateAllowsTrustedPluginAccountMenu(t *testing.T) {
	if err := validateNavigationCandidates(portalapi.PortalSpec{}, accountNavigationIndexFixture(t, "cn.vastplan.example.account-security", "secondary")); err != nil {
		t.Fatalf("可信插件应可挂载账户头像菜单: %v", err)
	}
	if err := validateNavigationCandidates(portalapi.PortalSpec{}, accountNavigationIndexFixture(t, "cn.vastplan.example.account-security", "primary")); err == nil || !strings.Contains(err.Error(), "secondary") {
		t.Fatalf("账户头像菜单不在 secondary 区域应拒绝: %v", err)
	}
}

func navigationIndexFixture(t *testing.T) pluginv1.ContributionIndexSnapshot {
	t.Helper()
	descriptor, err := json.Marshal(pluginv1.FrontendNavigationCatalog{ID: "main", Contract: pluginv1.FrontendNavigationContract, Nodes: []pluginv1.FrontendNavigationNode{
		{ID: "root", Zone: "primary"},
		{ID: "child", Zone: "primary", Parent: &pluginv1.FrontendNavigationParent{NodeID: "root", Mode: "required"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return pluginv1.ContributionIndexSnapshot{Contributions: []pluginv1.IndexedContribution{{
		Kind: pluginv1.FrontendNavigationContributionKind, Surface: "frontend", ID: "main", Contract: pluginv1.FrontendNavigationContract,
		Owner: pluginv1.PluginArtifactIdentity{Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.test.navigation", Version: "1.0.0", Channel: "stable"}}, Descriptor: descriptor,
	}}}
}

func accountNavigationIndexFixture(t *testing.T, owner, zone string) pluginv1.ContributionIndexSnapshot {
	t.Helper()
	descriptor, err := json.Marshal(pluginv1.FrontendNavigationCatalog{ID: "main", Contract: pluginv1.FrontendNavigationContract, Nodes: []pluginv1.FrontendNavigationNode{{
		ID: "profile", Zone: zone, Parent: &pluginv1.FrontendNavigationParent{PluginID: "vastplan.host", NodeID: "account", Mode: "required"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return pluginv1.ContributionIndexSnapshot{Contributions: []pluginv1.IndexedContribution{{
		Kind: pluginv1.FrontendNavigationContributionKind, Surface: "frontend", ID: "main", Contract: pluginv1.FrontendNavigationContract,
		Owner: pluginv1.PluginArtifactIdentity{Ref: pluginv1.ArtifactRef{PluginID: owner, Version: "1.0.0", Channel: "stable"}}, Descriptor: descriptor,
	}}}
}
