package edge

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortalAssetsServesNonceProtectedShellAndFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `<script type="importmap" nonce="__VASTPLAN_CSP_NONCE__">{"imports":{}}</script><script type="module" nonce="__VASTPLAN_CSP_NONCE__" src="/assets/portal.js"></script>`
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "portal.js"), []byte("export {};"), 0o600); err != nil {
		t.Fatal(err)
	}
	assets, err := NewPortalAssets(dir)
	if err != nil {
		t.Fatal(err)
	}

	shell := httptest.NewRecorder()
	assets.ServeHTTP(shell, httptest.NewRequest(http.MethodGet, "/settings/portals", nil))
	if shell.Code != http.StatusOK || strings.Contains(shell.Body.String(), portalNoncePlaceholder) {
		t.Fatalf("SPA shell 未安全注入 nonce: status=%d body=%s", shell.Code, shell.Body.String())
	}
	if csp := shell.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self' blob: 'nonce-") || strings.Contains(csp, "script-src 'unsafe-inline'") {
		t.Fatalf("Portal CSP 未限制脚本来源: %q", csp)
	}

	asset := httptest.NewRecorder()
	assets.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/portal.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "export {};" || !strings.HasPrefix(asset.Header().Get("ETag"), `"sha256-`) {
		t.Fatalf("静态资产响应无效: status=%d headers=%v body=%s", asset.Code, asset.Header(), asset.Body.String())
	}

	for _, path := range []string{"/assets/", "/v1/unknown"} {
		w := httptest.NewRecorder()
		assets.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("路径 %s 不得回落到 shell: %d", path, w.Code)
		}
	}
}

func TestPortalAssetsRejectsIndexWithoutNoncePlaceholder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<main></main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPortalAssets(dir); err == nil {
		t.Fatal("缺少 CSP nonce 占位符的静态产物必须拒绝")
	}
}
