package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrontendHMRInstallsDigestBoundModuleAndOverlaysRuntime(t *testing.T) {
	directory := t.TempDir()
	modulePath := filepath.Join(directory, "cn.vastplan.feature.js")
	content := []byte(`export default { register() {} }`)
	if err := os.WriteFile(modulePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	sha := hex.EncodeToString(digest[:])
	manifestPath := filepath.Join(directory, "manifest.json")
	manifest := map[string]any{"version": 1, "modules": []map[string]string{{"id": "cn.vastplan.feature", "entry": "frontend/dist/index.js", "file": modulePath, "sha256": sha}}}
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie("vastplan_session")
		if err != nil || cookie.Value != devAdminToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if request.URL.Query().Get("path") != "/operations" {
			http.Error(w, "portal path missing", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"portal":  map[string]any{"revision": 7},
			"modules": []map[string]any{{"id": "cn.vastplan.feature", "version": "1.0.0", "entry": "frontend/dist/index.js", "url": "/v1/portal-modules/7/" + strings.Repeat("b", 64) + ".js", "sha256": strings.Repeat("b", 64), "packageSha256": strings.Repeat("c", 64)}},
		})
	}))
	defer upstream.Close()

	hmr := &frontendHMR{
		portalListen: strings.TrimPrefix(upstream.URL, "https://"), current: map[string]frontendHMRModule{}, objects: map[string]frontendHMRObject{}, subscribers: map[chan frontendHMREvent]struct{}{},
	}
	if err := hmr.install(manifestPath); err != nil {
		t.Fatalf("install: %v", err)
	}
	if generation, lastError := hmr.status(); generation != 1 || lastError != "" {
		t.Fatalf("status = %d, %q", generation, lastError)
	}

	moduleRequest := httptest.NewRequest(http.MethodGet, "/__vastplan_dev/modules/"+sha+".js", nil)
	moduleRequest.RemoteAddr = "127.0.0.1:43210"
	moduleResponse := httptest.NewRecorder()
	hmr.module(moduleResponse, moduleRequest)
	if moduleResponse.Code != http.StatusOK || moduleResponse.Body.String() != string(content) || moduleResponse.Header().Get("X-VastPlan-Module-SHA256") != sha {
		t.Fatalf("module response code=%d body=%q headers=%v", moduleResponse.Code, moduleResponse.Body.String(), moduleResponse.Header())
	}

	runtimeRequest := httptest.NewRequest(http.MethodGet, "/__vastplan_dev/runtime", nil)
	runtimeRequest.RemoteAddr = "127.0.0.1:43210"
	runtimeResponse := httptest.NewRecorder()
	hmr.runtime(runtimeResponse, runtimeRequest)
	if runtimeResponse.Code != http.StatusOK {
		t.Fatalf("runtime response: %d %s", runtimeResponse.Code, runtimeResponse.Body.String())
	}
	var runtime struct {
		Modules []map[string]any `json:"modules"`
	}
	if err := json.Unmarshal(runtimeResponse.Body.Bytes(), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.Modules[0]["url"] != "/__vastplan_dev/modules/"+sha+".js" || runtime.Modules[0]["sha256"] != sha || runtime.Modules[0]["packageSha256"] != strings.Repeat("c", 64) {
		t.Fatalf("unexpected overlay: %#v", runtime.Modules[0])
	}
}

func TestFrontendHMROverlaysPublicUIContractsWithTheSameAtomicGeneration(t *testing.T) {
	document := map[string]json.RawMessage{
		"portal":       json.RawMessage(`{"renderAdapter":{"uiContract":"^4.0.0"},"shell":{"uiContract":"^4.0.0"},"workbench":{"uiContract":"^4.0.0"}}`),
		"moduleGraphs": json.RawMessage(`[{"id":"cn.vastplan.render"},{"id":"cn.vastplan.renderer"},{"id":"cn.vastplan.shell"},{"id":"cn.vastplan.layout"},{"id":"cn.vastplan.workbench"}]`),
	}
	modules := map[string]frontendHMRModule{
		"cn.vastplan.render":    {UIContract: &frontendHMRUIContract{Family: frontendHMRUIFamilyRender, Range: "^5.0.0"}},
		"cn.vastplan.renderer":  {UIContract: &frontendHMRUIContract{Family: frontendHMRUIFamilyRender, Range: "^5.0.0"}},
		"cn.vastplan.shell":     {UIContract: &frontendHMRUIContract{Family: frontendHMRUIFamilyShell, Range: "^5.0.0"}},
		"cn.vastplan.layout":    {UIContract: &frontendHMRUIContract{Family: frontendHMRUIFamilyShell, Range: "^5.0.0"}},
		"cn.vastplan.workbench": {UIContract: &frontendHMRUIContract{Family: frontendHMRUIFamilyWorkbench, Range: "^5.0.0"}},
	}
	if err := overlayFrontendHMRUIContracts(document, modules); err != nil {
		t.Fatal(err)
	}
	var portal struct {
		RenderAdapter struct {
			UIContract string `json:"uiContract"`
		} `json:"renderAdapter"`
		Shell struct {
			UIContract string `json:"uiContract"`
		} `json:"shell"`
		Workbench struct {
			UIContract string `json:"uiContract"`
		} `json:"workbench"`
	}
	if err := json.Unmarshal(document["portal"], &portal); err != nil {
		t.Fatal(err)
	}
	if portal.RenderAdapter.UIContract != "^5.0.0" || portal.Shell.UIContract != "^5.0.0" || portal.Workbench.UIContract != "^5.0.0" {
		t.Fatalf("UI contract overlay incomplete: %#v", portal)
	}
}

func TestFrontendHMRRejectsPartiallySynchronizedUIContractFamily(t *testing.T) {
	document := map[string]json.RawMessage{
		"portal":       json.RawMessage(`{"renderAdapter":{"uiContract":"^4.0.0"}}`),
		"moduleGraphs": json.RawMessage(`[{"id":"cn.vastplan.render"},{"id":"cn.vastplan.renderer"}]`),
	}
	modules := map[string]frontendHMRModule{
		"cn.vastplan.render":   {UIContract: &frontendHMRUIContract{Family: frontendHMRUIFamilyRender, Range: "^5.0.0"}},
		"cn.vastplan.renderer": {UIContract: &frontendHMRUIContract{Family: frontendHMRUIFamilyRender, Range: "^4.0.0"}},
	}
	if err := overlayFrontendHMRUIContracts(document, modules); err == nil || !strings.Contains(err.Error(), "UI 契约未同步") {
		t.Fatalf("partial UI contract error = %v", err)
	}
}

func TestFrontendUIContractSourcesStaySynchronized(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateFrontendUIContractSources(root); err != nil {
		t.Fatal(err)
	}
}

func TestFrontendHMROverlaysGraphNativeRuntimeWithoutCandidateVersionValidation(t *testing.T) {
	directory := t.TempDir()
	pluginID := "cn.vastplan.feature"
	pluginRoot := filepath.Join(directory, pluginID)
	entryPath := filepath.Join(pluginRoot, "frontend", "dist", "index.js")
	chunkPath := filepath.Join(pluginRoot, "frontend", "dist", "chunks", "lazy.js")
	if err := os.MkdirAll(filepath.Dir(chunkPath), 0o700); err != nil {
		t.Fatal(err)
	}
	entryContent := []byte(`export { value } from "./chunks/lazy.js";`)
	chunkContent := []byte(`export const value = "development";`)
	if err := os.WriteFile(entryPath, entryContent, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chunkPath, chunkContent, 0o600); err != nil {
		t.Fatal(err)
	}
	entryDigest := sha256.Sum256(entryContent)
	chunkDigest := sha256.Sum256(chunkContent)
	entrySHA, chunkSHA := hex.EncodeToString(entryDigest[:]), hex.EncodeToString(chunkDigest[:])
	graphDigest := strings.Repeat("d", 64)
	manifest := map[string]any{"version": 1, "modules": []map[string]any{{
		"id": pluginID, "entry": "frontend/dist/index.js", "file": entryPath, "sha256": entrySHA,
		"graph": map[string]any{
			"target": "browser", "entry": "frontend/dist/index.js", "digest": graphDigest, "externals": []string{"react"},
			"nodes": []map[string]any{
				{"path": "frontend/dist/index.js", "sha256": entrySHA, "size": len(entryContent), "mediaType": "text/javascript", "purpose": "entry", "dependencies": []map[string]string{{"specifier": "./chunks/lazy.js", "path": "frontend/dist/chunks/lazy.js", "kind": "static"}}},
				{"path": "frontend/dist/chunks/lazy.js", "sha256": chunkSHA, "size": len(chunkContent), "mediaType": "text/javascript", "purpose": "chunk", "dependencies": []any{}},
			},
		},
	}}}
	manifestRaw, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestRaw, 0o600); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"portal":{"revision":7},"moduleGraphs":[{"id":"cn.vastplan.feature","version":"2024.999.0","channel":"stable","target":"browser","entry":"frontend/dist/old.js","digest":"` + strings.Repeat("a", 64) + `","packageSha256":"` + strings.Repeat("b", 64) + `","externals":[],"nodes":[{"path":"frontend/dist/old.js","url":"/v1/portal-modules/7/` + strings.Repeat("c", 64) + `.js","sha256":"` + strings.Repeat("c", 64) + `","size":1,"mediaType":"text/javascript","purpose":"entry","dependencies":[]}]}]}`)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie("vastplan_session")
		if err != nil || cookie.Value != devAdminToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()

	hmr := &frontendHMR{
		portalListen: strings.TrimPrefix(upstream.URL, "https://"), current: map[string]frontendHMRModule{}, objects: map[string]frontendHMRObject{}, subscribers: map[chan frontendHMREvent]struct{}{},
	}
	if err := hmr.install(manifestPath); err != nil {
		t.Fatalf("install graph candidate: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/__vastplan_dev/runtime?path=%2Foperations", nil)
	request.RemoteAddr = "127.0.0.1:43210"
	response := httptest.NewRecorder()
	hmr.runtime(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("runtime response: %d %s", response.Code, response.Body.String())
	}
	var runtime struct {
		Modules      json.RawMessage `json:"modules"`
		ModuleGraphs []struct {
			ID, Version, Digest, PackageSHA256 string
			Nodes                              []frontendHMRGraphNode
		} `json:"moduleGraphs"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.Modules != nil {
		t.Fatalf("图谱 RuntimeSpec 不得被降级为 modules: %s", response.Body.String())
	}
	graph := runtime.ModuleGraphs[0]
	if graph.ID != pluginID || graph.Version != "2024.999.0" || graph.PackageSHA256 != strings.Repeat("b", 64) || graph.Digest != graphDigest || len(graph.Nodes) != 2 {
		t.Fatalf("开发图谱覆盖不得依赖候选版本并必须保留活动身份: %#v", graph)
	}
	if graph.Nodes[0].URL != "/__vastplan_dev/modules/"+entrySHA+".js" || graph.Nodes[1].URL != "/__vastplan_dev/modules/"+chunkSHA+".js" {
		t.Fatalf("开发图谱节点 URL 未覆盖: %#v", graph.Nodes)
	}
	moduleRequest := httptest.NewRequest(http.MethodGet, graph.Nodes[1].URL, nil)
	moduleRequest.RemoteAddr = "127.0.0.1:43210"
	moduleResponse := httptest.NewRecorder()
	hmr.module(moduleResponse, moduleRequest)
	if moduleResponse.Code != http.StatusOK || moduleResponse.Body.String() != string(chunkContent) || moduleResponse.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("graph node response code=%d body=%q headers=%v", moduleResponse.Code, moduleResponse.Body.String(), moduleResponse.Header())
	}
}

func TestFrontendHMRRejectsNonLoopbackAndEscapingManifest(t *testing.T) {
	hmr := &frontendHMR{current: map[string]frontendHMRModule{}, objects: map[string]frontendHMRObject{}, subscribers: map[chan frontendHMREvent]struct{}{}}
	request := httptest.NewRequest(http.MethodGet, "/__vastplan_dev/modules/"+strings.Repeat("a", 64)+".js", nil)
	request.RemoteAddr = "203.0.113.4:1234"
	response := httptest.NewRecorder()
	hmr.module(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-loopback response = %d", response.Code)
	}

	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escape.js")
	content := []byte("export default {}")
	if err := os.WriteFile(outside, content, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	manifest := map[string]any{"version": 1, "modules": []map[string]string{{"id": "cn.vastplan.escape", "entry": "frontend/dist/index.js", "file": outside, "sha256": hex.EncodeToString(digest[:])}}}
	raw, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := hmr.install(manifestPath); err == nil || !strings.Contains(err.Error(), "路径或身份无效") {
		t.Fatalf("escaping manifest error = %v", err)
	}
}

func TestFrontendHMRSeparatesPluginAndHostSourceChanges(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"extensions/plugins/cn.vastplan.feature/frontend/src/index.ts":                "plugin-v1",
		"examples/plugins/cn.vastplan.example.frontend.gallery/frontend/src/index.ts": "example-v1",
		"extensions/sdk/ts/platform-admin/src/index.ts":                               "admin-v1",
		"extensions/sdk/ts/platform-admin/package.json":                               "{}",
		"core/kernels/frontend/src/browser.tsx":                                       "host-v1",
		"core/kernels/frontend/static/index.html":                                     "host-v1",
		"core/kernels/frontend/package.json":                                          "{}",
		"extensions/sdk/ts/icon-catalog/src/index.ts":                                 "icon-catalog-v1",
		"extensions/sdk/ts/icon-catalog/package.json":                                 "{}",
		"extensions/sdk/ts/ui-primitives/src/index.ts":                                "ui-primitives-v1",
		"extensions/sdk/ts/ui-primitives/package.json":                                "{}",
		"extensions/sdk/ts/rjsf-csp-validator/src/index.ts":                           "rjsf-validator-v1",
		"extensions/sdk/ts/rjsf-csp-validator/package.json":                           "{}",
		"extensions/sdk/ts/ui-contract/src/index.ts":                                  "contract-v1",
		"extensions/sdk/ts/ui-contract/package.json":                                  "{}",
		"extensions/sdk/ts/workbench-sdk/src/index.ts":                                "workbench-v1",
		"extensions/sdk/ts/workbench-sdk/package.json":                                "{}",
		"engineering/tools/build-frontend.sh":                                         "build-v1",
		"engineering/tools/build-frontend-plugins.mjs":                                "build-v1",
		"engineering/tools/check-ant-icon-catalog-on-demand.mjs":                      "catalog-check-v1",
		"engineering/tools/frontend-module-graph.mjs":                                 "graph-v1",
		"engineering/tools/frontend-server-build.mjs":                                 "server-v1",
		"package.json":        "{}",
		"pnpm-lock.yaml":      "lockfileVersion: 1",
		"pnpm-workspace.yaml": "packages: []",
		"tsconfig.base.json":  "{}",
	}
	write := func(relative, content string) {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for relative, content := range files {
		write(relative, content)
	}
	hmr := &frontendHMR{root: root}
	initial, err := hmr.sourceSignatures()
	if err != nil {
		t.Fatal(err)
	}
	write("extensions/plugins/cn.vastplan.feature/frontend/src/index.ts", "plugin-v2")
	pluginChange, err := hmr.sourceSignatures()
	if err != nil {
		t.Fatal(err)
	}
	if pluginChange.plugins == initial.plugins || pluginChange.host != initial.host {
		t.Fatalf("plugin change signatures = %#v, initial = %#v", pluginChange, initial)
	}
	write("examples/plugins/cn.vastplan.example.frontend.gallery/frontend/src/index.ts", "example-v2")
	exampleChange, err := hmr.sourceSignatures()
	if err != nil || exampleChange.plugins == pluginChange.plugins || exampleChange.host != pluginChange.host {
		t.Fatalf("example change signatures = %#v, plugin = %#v, err=%v", exampleChange, pluginChange, err)
	}
	pluginChange = exampleChange
	write("extensions/sdk/ts/ui-primitives/src/index.ts", "ui-primitives-v2")
	hostChange, err := hmr.sourceSignatures()
	if err != nil {
		t.Fatal(err)
	}
	if hostChange.host == pluginChange.host || hostChange.plugins != pluginChange.plugins {
		t.Fatalf("host change signatures = %#v, plugin = %#v", hostChange, pluginChange)
	}
	write("extensions/sdk/ts/icon-catalog/src/index.ts", "icon-catalog-v2")
	iconCatalogChange, err := hmr.sourceSignatures()
	if err != nil {
		t.Fatal(err)
	}
	if iconCatalogChange.host == hostChange.host || iconCatalogChange.plugins != hostChange.plugins {
		t.Fatalf("icon catalog change signatures = %#v, host = %#v", iconCatalogChange, hostChange)
	}
	hostChange = iconCatalogChange
	write("extensions/sdk/ts/workbench-sdk/src/index.ts", "workbench-v2")
	workbenchChange, err := hmr.sourceSignatures()
	if err != nil {
		t.Fatal(err)
	}
	if workbenchChange.host == hostChange.host || workbenchChange.plugins != hostChange.plugins {
		t.Fatalf("workbench change signatures = %#v, host = %#v", workbenchChange, hostChange)
	}
	write("extensions/sdk/ts/rjsf-csp-validator/src/index.ts", "rjsf-validator-v2")
	validatorChange, err := hmr.sourceSignatures()
	if err != nil {
		t.Fatal(err)
	}
	if validatorChange.host == workbenchChange.host || validatorChange.plugins != workbenchChange.plugins {
		t.Fatalf("RJSF validator change signatures = %#v, workbench = %#v", validatorChange, workbenchChange)
	}
}

func TestChangedFrontendPluginsSelectsOnlyChangedPluginAndEscalatesSharedChanges(t *testing.T) {
	previous := frontendPluginWatchState{shared: "shared-v1", plugins: map[string]string{"cn.vastplan.a": "a-v1", "cn.vastplan.b": "b-v1"}}
	next := frontendPluginWatchState{shared: "shared-v1", plugins: map[string]string{"cn.vastplan.a": "a-v2", "cn.vastplan.b": "b-v1"}}
	changed, rebuildAll := changedFrontendPlugins(previous, next)
	if rebuildAll || len(changed) != 1 || changed[0] != "cn.vastplan.a" {
		t.Fatalf("changed=%v rebuildAll=%t", changed, rebuildAll)
	}
	next.shared = "shared-v2"
	if changed, rebuildAll := changedFrontendPlugins(previous, next); !rebuildAll || len(changed) != 0 {
		t.Fatalf("shared change must rebuild all: changed=%v rebuildAll=%t", changed, rebuildAll)
	}
}

func TestFrontendHMRPartialCandidatePreservesPreviousPluginOverlay(t *testing.T) {
	aDigest, bDigest := strings.Repeat("a", 64), strings.Repeat("b", 64)
	hmr := &frontendHMR{
		current:     map[string]frontendHMRModule{"cn.vastplan.a": {ID: "cn.vastplan.a", Digests: []string{aDigest}}},
		objects:     map[string]frontendHMRObject{aDigest: {Bytes: []byte("a"), MediaType: "text/javascript"}},
		subscribers: map[chan frontendHMREvent]struct{}{},
	}
	candidate := frontendHMRCandidate{
		current: map[string]frontendHMRModule{"cn.vastplan.b": {ID: "cn.vastplan.b", Digests: []string{bDigest}}},
		objects: map[string]frontendHMRObject{bDigest: {Bytes: []byte("b"), MediaType: "text/javascript"}},
	}
	hmr.commitCandidate(candidate, "generation", nil, false)
	if len(hmr.current) != 2 || hmr.current["cn.vastplan.a"].ID == "" || hmr.current["cn.vastplan.b"].ID == "" {
		t.Fatalf("partial commit lost overlays: %#v", hmr.current)
	}
}

func TestFrontendHMRCommitsHostAssetsAndModulesAsReload(t *testing.T) {
	updates := make(chan frontendHMREvent, 1)
	hmr := &frontendHMR{
		current:     map[string]frontendHMRModule{},
		objects:     map[string]frontendHMRObject{},
		subscribers: map[chan frontendHMREvent]struct{}{updates: {}},
		assets:      http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("old-vendor")) }),
	}
	module := frontendHMRModule{ID: "cn.vastplan.feature", SHA256: strings.Repeat("a", 64), Digests: []string{strings.Repeat("a", 64)}}
	object := frontendHMRObject{Bytes: []byte("new-plugin"), MediaType: "text/javascript"}
	assets := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("new-vendor-with-message")) })
	hmr.commitCandidate(frontendHMRCandidate{current: map[string]frontendHMRModule{module.ID: module}, objects: map[string]frontendHMRObject{module.SHA256: object}}, "reload", assets, true)

	event := <-updates
	if event.Name != "reload" {
		t.Fatalf("event = %#v", event)
	}
	request := httptest.NewRequest(http.MethodGet, "/assets/vendor/ui-primitives.js", nil)
	response := httptest.NewRecorder()
	hmr.portalAssets(response, request)
	if response.Body.String() != "new-vendor-with-message" || string(hmr.objects[module.SHA256].Bytes) != "new-plugin" {
		t.Fatalf("host/module commit was not atomic: body=%q module=%q", response.Body.String(), hmr.objects[module.SHA256].Bytes)
	}
}
