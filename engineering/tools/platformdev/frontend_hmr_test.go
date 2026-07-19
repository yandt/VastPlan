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
	modulePath := filepath.Join(directory, "com.vastplan.feature.js")
	content := []byte(`export default { register() {} }`)
	if err := os.WriteFile(modulePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	sha := hex.EncodeToString(digest[:])
	manifestPath := filepath.Join(directory, "manifest.json")
	manifest := map[string]any{"version": 1, "modules": []map[string]string{{"id": "com.vastplan.feature", "entry": "frontend/dist/index.js", "file": modulePath, "sha256": sha}}}
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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"portal":  map[string]any{"revision": 7},
			"modules": []map[string]any{{"id": "com.vastplan.feature", "version": "1.0.0", "entry": "frontend/dist/index.js", "url": "/v1/portal-modules/7/" + strings.Repeat("b", 64) + ".js", "sha256": strings.Repeat("b", 64), "packageSha256": strings.Repeat("c", 64)}},
		})
	}))
	defer upstream.Close()

	hmr := &frontendHMR{
		portalListen: strings.TrimPrefix(upstream.URL, "https://"), current: map[string]frontendHMRModule{}, objects: map[string][]byte{}, subscribers: map[chan frontendHMREvent]struct{}{},
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

	runtimeRequest := httptest.NewRequest(http.MethodGet, "/__vastplan_dev/runtime?path=%2Foperations", nil)
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

func TestFrontendHMRRejectsNonLoopbackAndEscapingManifest(t *testing.T) {
	hmr := &frontendHMR{current: map[string]frontendHMRModule{}, objects: map[string][]byte{}, subscribers: map[chan frontendHMREvent]struct{}{}}
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
	manifest := map[string]any{"version": 1, "modules": []map[string]string{{"id": "com.vastplan.escape", "entry": "frontend/dist/index.js", "file": outside, "sha256": hex.EncodeToString(digest[:])}}}
	raw, _ := json.Marshal(manifest)
	manifestPath := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := hmr.install(manifestPath); err == nil || !strings.Contains(err.Error(), "路径或身份无效") {
		t.Fatalf("escaping manifest error = %v", err)
	}
}
