package edge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func TestFrontendObjectURLUsesGovernedMediaExtension(t *testing.T) {
	digest := strings.Repeat("a", 64)
	cases := map[string]string{
		"text/javascript": ".js", "text/css": ".css", "application/json": ".json",
		"application/wasm": ".wasm", "image/svg+xml": ".bin",
	}
	for mediaType, extension := range cases {
		url := frontendObjectURL(7, digest, mediaType)
		if !strings.HasSuffix(url, digest+extension) {
			t.Fatalf("%s URL 扩展名错误: %s", mediaType, url)
		}
		_, parsed, ok := parseModulePath(url)
		if !ok || parsed != digest {
			t.Fatalf("内容寻址 URL 无法被 Handler 解析: %s", url)
		}
	}
	if _, _, ok := parseModulePath("/v1/portal-modules/7/" + digest + ".html"); ok {
		t.Fatal("未允许的前端对象扩展名必须拒绝")
	}
}

func TestMaterializeFrontendModuleGraphProjectsOnlyVerifiedBrowserNodes(t *testing.T) {
	entryPath, chunkPath := "frontend/dist/index.js", "frontend/dist/chunks/lazy.js"
	entry, chunk := []byte("import('./chunks/lazy.js');\n"), []byte("export const lazy = true;\n")
	entryDigest, chunkDigest := sha256.Sum256(entry), sha256.Sum256(chunk)
	graph := pluginv1.FrontendModuleGraph{SchemaVersion: "v1", Target: "browser", Entry: entryPath, Externals: []string{"react"}, Nodes: []pluginv1.FrontendModuleNode{
		{Path: entryPath, SHA256: hex.EncodeToString(entryDigest[:]), Size: int64(len(entry)), MediaType: "text/javascript", Purpose: "entry", Dependencies: []pluginv1.FrontendModuleDependency{{Specifier: "chunks/lazy.js", Path: chunkPath, Kind: "dynamic"}}},
		{Path: chunkPath, SHA256: hex.EncodeToString(chunkDigest[:]), Size: int64(len(chunk)), MediaType: "text/javascript", Purpose: "chunk", Dependencies: []pluginv1.FrontendModuleDependency{}},
	}}
	graph.Digest = graph.ComputedDigest()
	manifest := pluginv1.Manifest{
		ID: "cn.vastplan.product.test.delivery-graph", Name: "graph", Description: "graph", Version: "1.0.0", Publisher: "vastplan",
		Engines: map[string]string{"frontend": "^1.0"}, Activation: []string{"onPortalStartup"}, Entry: map[string]string{"frontend": entryPath},
		FrontendModuleGraphs: &pluginv1.FrontendModuleGraphs{Browser: &graph}, Contributes: map[string]json.RawMessage{"frontend": json.RawMessage(`{"views":[]}`)},
	}
	directory := t.TempDir()
	writeGraphFixture(t, directory, "vastplan.plugin.json", mustJSON(t, manifest))
	writeGraphFixture(t, directory, entryPath, entry)
	writeGraphFixture(t, directory, chunkPath, chunk)
	packageBytes, parsed, err := pluginservice.PackageDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe("stable", packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	projected, assets, err := materializeFrontendModuleGraph(verifiedPortalPlugin{
		ref: portalapi.PluginRef{ID: manifest.ID, Version: manifest.Version, Channel: "stable"}, artifact: artifact, packageBytes: packageBytes, manifest: parsed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if projected.Digest != graph.Digest || len(projected.Nodes) != 2 || len(assets) != 2 || projected.Nodes[0].Dependencies[0].Specifier != "chunks/lazy.js" {
		t.Fatalf("Module Graph 投影不完整: graph=%+v assets=%d", projected, len(assets))
	}
	store, err := newFrontendDeliveryStore("")
	if err != nil {
		t.Fatal(err)
	}
	spec := portalapi.PortalSpec{Revision: 3, ID: "graph", TenantID: "tenant-a"}
	if err := store.put("tenant-a", spec, portalapi.RuntimeSpec{Portal: spec, ModuleGraphs: []portalapi.FrontendModuleGraph{projected}}, assets); err != nil {
		t.Fatal(err)
	}
	runtime, err := store.runtime("tenant-a", spec)
	if err != nil || len(runtime.Modules) != 0 || len(runtime.ModuleGraphs) != 1 || runtime.ModuleGraphs[0].Nodes[0].URL == "" {
		t.Fatalf("图交付快照无效: runtime=%+v err=%v", runtime, err)
	}
	asset, err := store.module("tenant-a", spec, chunkDigestString(chunkDigest))
	if err != nil || string(asset.Content) != string(chunk) || asset.Descriptor.MediaType != "text/javascript" {
		t.Fatalf("图节点内容读取失败: asset=%+v err=%v", asset.Descriptor, err)
	}
}

func writeGraphFixture(t *testing.T, root, name string, content []byte) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func chunkDigestString(value [sha256.Size]byte) string { return hex.EncodeToString(value[:]) }
