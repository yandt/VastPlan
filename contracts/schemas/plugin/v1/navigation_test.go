package pluginv1

import (
	"strings"
	"testing"
)

func TestFrontendNavigationCatalogValidatesOwnedInternationalizedTree(t *testing.T) {
	manifest, err := ParseManifest(navigationManifest(`{
      "navigations":[{"id":"main","contract":"1.0.0","nodes":[
        {"id":"resources","zone":"primary","label":{"key":"navigation.resources","fallback":"资源与配置"},"icon":{"kind":"semantic","name":"settings"},"order":10},
        {"id":"databases","zone":"primary","label":{"key":"navigation.databases","fallback":"数据库"},"icon":{"kind":"custom","name":"database"},"parent":{"nodeId":"resources","mode":"required"}}
      ],"icons":[{"id":"database","motion":"pulse","states":{"normal":{"viewBox":"0 0 24 24","nodes":[{"tag":"path","d":"M1 1L23 1L23 23L1 23Z","tone":"primary"}]}}}]}]
    }`, ""))
	if err != nil {
		t.Fatalf("解析导航 Manifest: %v", err)
	}
	catalog, err := FrontendNavigationCatalogFor(manifest)
	if err != nil || catalog == nil || len(catalog.Nodes) != 2 || catalog.Nodes[1].Label.Key != "navigation.databases" {
		t.Fatalf("导航目录投影错误: %+v %v", catalog, err)
	}
}

func TestFrontendNavigationCatalogRejectsLegacyMenus(t *testing.T) {
	_, err := ParseManifest(navigationManifest(`{"menus":[{"id":"legacy","title":"Legacy"}]}`, ""))
	if err == nil || !strings.Contains(err.Error(), "menus") {
		t.Fatalf("旧 frontend.menus 应被拒绝: %v", err)
	}
}

func TestFrontendNavigationCatalogRequiresDeclaredCrossPluginParent(t *testing.T) {
	_, err := ParseManifest(navigationManifest(`{
      "navigations":[{"id":"main","contract":"1.0.0","nodes":[
        {"id":"child","zone":"primary","label":{"key":"navigation.child","fallback":"子菜单"},"icon":{"kind":"semantic","name":"menu"},"parent":{"pluginId":"cn.vastplan.parent","nodeId":"root","mode":"required"}}
      ],"icons":[]}]
    }`, ""))
	if err == nil || !strings.Contains(err.Error(), "未声明依赖") {
		t.Fatalf("required 跨插件父级应要求 Manifest 依赖: %v", err)
	}
	if _, err := ParseManifest(navigationManifest(`{
      "navigations":[{"id":"main","contract":"1.0.0","nodes":[
        {"id":"child","zone":"primary","label":{"key":"navigation.child","fallback":"子菜单"},"icon":{"kind":"semantic","name":"menu"},"parent":{"pluginId":"cn.vastplan.parent","nodeId":"root","mode":"required"}}
      ],"icons":[]}]
    }`, `,"dependencies":{"cn.vastplan.parent":"^1.0.0"}`)); err != nil {
		t.Fatalf("声明依赖后 required 父级应有效: %v", err)
	}
}

func TestFrontendNavigationCatalogValidatesOptionalFallback(t *testing.T) {
	valid := `{
      "navigations":[{"id":"main","contract":"1.0.0","nodes":[
        {"id":"fallback","zone":"primary","label":{"key":"navigation.fallback","fallback":"回退"},"icon":{"kind":"semantic","name":"menu"}},
        {"id":"child","zone":"primary","label":{"key":"navigation.child","fallback":"子菜单"},"icon":{"kind":"semantic","name":"menu"},"parent":{"pluginId":"cn.vastplan.optional","nodeId":"root","mode":"optional","fallbackNodeId":"fallback"}}
      ],"icons":[]}]
    }`
	if _, err := ParseManifest(navigationManifest(valid, "")); err != nil {
		t.Fatalf("optional 自有根回退应有效: %v", err)
	}
	invalid := strings.Replace(valid, `"fallbackNodeId":"fallback"`, `"fallbackNodeId":"missing"`, 1)
	if _, err := ParseManifest(navigationManifest(invalid, "")); err == nil || !strings.Contains(err.Error(), "自有根节点") {
		t.Fatalf("optional 未知回退应拒绝: %v", err)
	}
}

func TestNavigationSVGSourceIsAuthoringOnly(t *testing.T) {
	manifest, err := ParseManifest(navigationManifest(`{
      "navigations":[{"id":"main","contract":"1.0.0","nodes":[
        {"id":"custom","zone":"primary","label":{"key":"navigation.custom","fallback":"自定义"},"icon":{"kind":"custom","name":"custom"}}
      ],"icons":[{"id":"custom","motion":"draw","sources":{"normal":"frontend/icons/navigation/custom.svg"}}]}]
    }`, ""))
	if err != nil {
		t.Fatalf("源码 Manifest 应允许受限 SVG source: %v", err)
	}
	if err := ValidatePackagedNavigationCatalog(manifest); err == nil || !strings.Contains(err.Error(), "不得保留原始 SVG") {
		t.Fatalf("签名制品必须拒绝未归一化 source: %v", err)
	}
}

func navigationManifest(frontend, suffix string) []byte {
	return []byte(`{
      "id":"cn.vastplan.demo-navigation","name":"Navigation","description":"Navigation fixture","version":"1.0.0","publisher":"vastplan",
      "engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/dist/index.js"},
      "contributes":{"frontend":` + frontend + `}` + suffix + `
    }`)
}
