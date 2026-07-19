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
	if err := origin.put("tenant-a", spec, assets); err != nil {
		t.Fatal(err)
	}
	if err := cache.prefetchFrom(origin, "tenant-a", spec); err != nil {
		t.Fatalf("相同内容的不同插件必须复用对象但保留各自 Runtime 描述符: %v", err)
	}
	runtime, err := cache.runtime("tenant-a", spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.Modules) != 2 || runtime.Modules[0].PackageSHA256 == runtime.Modules[1].PackageSHA256 {
		t.Fatalf("预取后 Runtime 描述符丢失: %+v", runtime.Modules)
	}
}
