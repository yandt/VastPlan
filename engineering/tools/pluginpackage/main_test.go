package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestCopyTreeSkipsPythonBytecode(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "main.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(source, "__pycache__")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "main.pyc"), []byte("bytecode"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(source, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "main.py")); err != nil {
		t.Fatal("Python 源文件必须进入制品")
	}
	if _, err := os.Stat(filepath.Join(target, "__pycache__")); !os.IsNotExist(err) {
		t.Fatalf("Python 字节码缓存不得进入制品: %v", err)
	}
}

func TestCopyTreeSkipsNodeModules(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	modules := filepath.Join(source, "frontend", "node_modules", "dependency")
	if err := os.MkdirAll(modules, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modules, "index.js"), []byte("dependency"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "vastplan.plugin.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(source, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "frontend", "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("node_modules 不得进入插件制品: %v", err)
	}
}

func TestStagePackageInjectsSignedDynamicGoFingerprint(t *testing.T) {
	source := t.TempDir()
	manifest := []byte(`{
  "id":"cn.vastplan.foundation.test.dynamic","name":"dynamic","description":"dynamic","version":"1.0.0","publisher":"vastplan",
  "engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"native","minimumIsolation":"trusted-process",
    "dynamicGo":{"entry":"backend/plugin.so","abi":"vastplan.dynamic-go.v1","required":true}}},
  "activation":["onStartup"],"entry":{"backend":"backend/plugin"},
  "contributes":{"backend":{"tools":[{"id":"foundation.test.dynamic.tool","service_role":"backend"}]}}
}`)
	if err := os.WriteFile(filepath.Join(source, "vastplan.plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	backend, dynamic := filepath.Join(t.TempDir(), "plugin"), filepath.Join(t.TempDir(), "plugin.so")
	if err := os.WriteFile(backend, []byte("process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dynamic, []byte("module"), 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint := strings.Repeat("a", 64)
	staged, cleanup := stagePackage(source, backend, "", "", "", dynamic, fingerprint, "", "")
	defer cleanup()
	raw, err := os.ReadFile(filepath.Join(staged, "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Execution.Backend.DynamicGo.Fingerprint != fingerprint || !got.Execution.Backend.DynamicGo.Required {
		t.Fatalf("签名清单没有冻结 dynamic-go 构建指纹: %+v", got.Execution.Backend.DynamicGo)
	}
}

func TestStagePackageInjectsFrontendBundleAtManifestEntry(t *testing.T) {
	source := t.TempDir()
	manifest := []byte(`{
  "id":"cn.vastplan.product.test.frontend","name":"frontend","description":"frontend","version":"1.0.0","publisher":"vastplan",
  "engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/dist/index.js"},
  "contributes":{"frontend":{"views":[{"id":"test.frontend","title":"Test"}]}}
}`)
	if err := os.WriteFile(filepath.Join(source, "vastplan.plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "index.js")
	content := []byte("export default { register() {} };\n")
	if err := os.WriteFile(bundle, content, 0o644); err != nil {
		t.Fatal(err)
	}
	staged, cleanup := stagePackage(source, "", bundle, "", "", "", "", "", "")
	defer cleanup()
	got, err := os.ReadFile(filepath.Join(staged, "frontend", "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("frontend bundle 注入内容不一致: %q", got)
	}
}

func TestStagePackageInjectsVerifiedFrontendModuleGraph(t *testing.T) {
	source := t.TempDir()
	manifest := []byte(`{
  "id":"cn.vastplan.product.test.graph","name":"graph","description":"graph","version":"1.0.0","publisher":"vastplan",
  "engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/dist/index.js","frontendServer":"frontend/dist/server.js"},
  "contributes":{"frontend":{"views":[{"id":"test.graph","title":"Test"}]}}
}`)
	if err := os.WriteFile(filepath.Join(source, "vastplan.plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	buildRoot := t.TempDir()
	entryPath, chunkPath, serverPath := "frontend/dist/index.js", "frontend/dist/chunks/lazy.js", "frontend/dist/server.js"
	entry, chunk, server := []byte("import('./chunks/lazy.js');\n"), []byte("export const lazy = true;\n"), []byte("export default { render() { return { html: '' }; } };\n")
	for name, content := range map[string][]byte{entryPath: entry, chunkPath: chunk, serverPath: server} {
		filename := filepath.Join(buildRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entryDigest, chunkDigest := sha256.Sum256(entry), sha256.Sum256(chunk)
	graph := pluginv1.FrontendModuleGraph{SchemaVersion: "v1", Target: "browser", Entry: entryPath, Externals: []string{}, Nodes: []pluginv1.FrontendModuleNode{
		{Path: entryPath, SHA256: hex.EncodeToString(entryDigest[:]), Size: int64(len(entry)), MediaType: "text/javascript", Purpose: "entry", Dependencies: []pluginv1.FrontendModuleDependency{{Specifier: "chunks/lazy.js", Path: chunkPath, Kind: "dynamic"}}},
		{Path: chunkPath, SHA256: hex.EncodeToString(chunkDigest[:]), Size: int64(len(chunk)), MediaType: "text/javascript", Purpose: "chunk", Dependencies: []pluginv1.FrontendModuleDependency{}},
	}}
	graph.Digest = graph.ComputedDigest()
	serverDigest := sha256.Sum256(server)
	serverGraph := pluginv1.FrontendModuleGraph{SchemaVersion: "v1", Target: "server", Entry: serverPath, Externals: []string{"stream"}, Nodes: []pluginv1.FrontendModuleNode{
		{Path: serverPath, SHA256: hex.EncodeToString(serverDigest[:]), Size: int64(len(server)), MediaType: "text/javascript", Purpose: "entry", Dependencies: []pluginv1.FrontendModuleDependency{}},
	}}
	serverGraph.Digest = serverGraph.ComputedDigest()
	graphRaw, err := json.Marshal(graph)
	if err != nil {
		t.Fatal(err)
	}
	graphFile := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(graphFile, graphRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	serverGraphRaw, err := json.Marshal(serverGraph)
	if err != nil {
		t.Fatal(err)
	}
	serverGraphFile := filepath.Join(t.TempDir(), "server-graph.json")
	if err := os.WriteFile(serverGraphFile, serverGraphRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	staged, cleanup := stagePackageWithGraphs(source, "", "", graphFile, serverGraphFile, buildRoot, "", "", "", "")
	defer cleanup()
	raw, err := os.ReadFile(filepath.Join(staged, "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.FrontendModuleGraphs == nil || parsed.FrontendModuleGraphs.Browser.Digest != graph.Digest || parsed.FrontendModuleGraphs.Server == nil || parsed.FrontendModuleGraphs.Server.Digest != serverGraph.Digest {
		t.Fatalf("签名清单未同时绑定 browser/server Module Graph: %+v", parsed.FrontendModuleGraphs)
	}
	got, err := os.ReadFile(filepath.Join(staged, filepath.FromSlash(chunkPath)))
	if err != nil || string(got) != string(chunk) {
		t.Fatalf("Module Graph chunk 未进入制品: %q %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(staged, filepath.FromSlash(serverPath)))
	if err != nil || string(got) != string(server) {
		t.Fatalf("Server Module Graph entry 未进入制品: %q %v", got, err)
	}
}
