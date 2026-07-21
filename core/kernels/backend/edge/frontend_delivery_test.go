package edge

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func TestFrontendDeliveryPrefetchKeepsRuntimeDescriptorForSharedContent(t *testing.T) {
	root := t.TempDir()
	origin, err := newFrontendDeliveryStore(filepath.Join(root, "origin"))
	if err != nil {
		t.Fatal(err)
	}
	cache, err := newFrontendDeliveryStore(filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(`export default {};`)
	digest := sha256.Sum256(content)
	sha := hex.EncodeToString(digest[:])
	spec := portalapi.PortalSpec{Revision: 1, ID: "operations", TenantID: "tenant-a"}
	assets := []FrontendModuleAsset{
		{Descriptor: portalapi.FrontendModule{PluginRef: portalapi.PluginRef{ID: "cn.vastplan.one", Version: "1.0.0"}, SHA256: sha, PackageSHA256: strings.Repeat("a", 64)}, Content: content},
		{Descriptor: portalapi.FrontendModule{PluginRef: portalapi.PluginRef{ID: "cn.vastplan.two", Version: "1.0.0"}, SHA256: sha, PackageSHA256: strings.Repeat("b", 64)}, Content: content},
	}
	runtime := portalapi.RuntimeSpec{Portal: spec, Modules: []portalapi.FrontendModule{assets[0].Descriptor, assets[1].Descriptor}}
	if err := origin.put("tenant-a", spec, runtime, assets); err != nil {
		t.Fatal(err)
	}
	if err := cache.prefetchFrom(origin, "tenant-a", spec); err != nil {
		t.Fatalf("相同内容的不同插件必须复用对象但保留各自 Runtime 描述符: %v", err)
	}
	gotRuntime, err := cache.runtime("tenant-a", spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotRuntime.Modules) != 2 || gotRuntime.Modules[0].PackageSHA256 == gotRuntime.Modules[1].PackageSHA256 {
		t.Fatalf("预取后 Runtime 描述符丢失: %+v", gotRuntime.Modules)
	}
}

func TestFrontendDeliveryRuntimeDoesNotExposeMutableSnapshotSlices(t *testing.T) {
	store, err := newFrontendDeliveryStore("")
	if err != nil {
		t.Fatal(err)
	}
	spec := portalapi.PortalSpec{Revision: 9, ID: "immutable", TenantID: "tenant-a"}
	content := []byte("x")
	contentDigest := sha256.Sum256(content)
	digest := hex.EncodeToString(contentDigest[:])
	runtime := portalapi.RuntimeSpec{Portal: spec, ModuleGraphs: []portalapi.FrontendModuleGraph{{
		PluginRef: portalapi.PluginRef{ID: "cn.vastplan.graph", Version: "1.0.0"}, Target: "browser", Entry: "frontend/main.js", Digest: strings.Repeat("b", 64), PackageSHA256: strings.Repeat("c", 64),
		Externals: []string{"react"}, Nodes: []portalapi.FrontendModuleNode{{Path: "frontend/main.js", SHA256: digest, Size: 1, MediaType: "text/javascript", Purpose: "entry", Dependencies: []portalapi.FrontendModuleDependency{}}},
	}}}
	asset := FrontendModuleAsset{Descriptor: graphNodeDescriptor(runtime.ModuleGraphs[0], runtime.ModuleGraphs[0].Nodes[0]), Content: content}
	if err := store.put("tenant-a", spec, runtime, []FrontendModuleAsset{asset}); err != nil {
		t.Fatal(err)
	}
	first, err := store.runtime("tenant-a", spec)
	if err != nil {
		t.Fatal(err)
	}
	first.ModuleGraphs[0].Externals[0] = "mutated"
	first.ModuleGraphs[0].Nodes[0].URL = "/mutated"
	second, err := store.runtime("tenant-a", spec)
	if err != nil {
		t.Fatal(err)
	}
	if second.ModuleGraphs[0].Externals[0] != "react" || second.ModuleGraphs[0].Nodes[0].URL == "/mutated" {
		t.Fatalf("调用方修改污染了不可变交付快照: %+v", second.ModuleGraphs[0])
	}
}
