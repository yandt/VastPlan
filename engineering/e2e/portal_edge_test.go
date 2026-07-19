//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	portaledgecommand "cdsoft.com.cn/VastPlan/core/kernels/backend/commands/portaledge"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

// TestPortalEdgeHTTPSGovernanceEndToEnd exercises the deployable entrypoint,
// not an in-memory substitute: artifacts are packaged and installed, both
// policy and Composer run as child processes, and calls cross the HTTPS BFF.
func TestPortalEdgeHTTPSGovernanceEndToEnd(t *testing.T) {
	repositoryRoot := filepath.Join(t.TempDir(), "repository")
	repository, err := pluginservice.NewRepository(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	publishBuiltPlugin(t, repository,
		"./extensions/plugins/com.vastplan.foundation.security.portal-access-policy/backend",
		"extensions/plugins/com.vastplan.foundation.security.portal-access-policy/vastplan.plugin.json")
	publishBuiltPlugin(t, repository,
		"./extensions/plugins/com.vastplan.platform.configuration.portal-composer/backend",
		"extensions/plugins/com.vastplan.platform.configuration.portal-composer/vastplan.plugin.json")
	publishBuiltPlugin(t, repository,
		"./extensions/plugins/com.vastplan.foundation.security.interaction-access-policy/backend",
		"extensions/plugins/com.vastplan.foundation.security.interaction-access-policy/vastplan.plugin.json")
	publishBuiltPlugin(t, repository,
		"./extensions/plugins/com.vastplan.platform.interaction.broker/backend",
		"extensions/plugins/com.vastplan.platform.interaction.broker/vastplan.plugin.json")
	for _, plugin := range []struct{ packageDir, manifest string }{
		{"./extensions/plugins/com.vastplan.platform.configuration.global-settings/backend", "extensions/plugins/com.vastplan.platform.configuration.global-settings/vastplan.plugin.json"},
		{"./extensions/plugins/com.vastplan.platform.security.credentials/backend", "extensions/plugins/com.vastplan.platform.security.credentials/vastplan.plugin.json"},
		{"./extensions/plugins/com.vastplan.platform.data.relational.connection-manager/backend", "extensions/plugins/com.vastplan.platform.data.relational.connection-manager/vastplan.plugin.json"},
		{"./extensions/plugins/com.vastplan.platform.artifacts.repository/backend", "extensions/plugins/com.vastplan.platform.artifacts.repository/vastplan.plugin.json"},
	} {
		publishBuiltPlugin(t, repository, plugin.packageDir, plugin.manifest)
	}
	publishPortalFrontendPlugin(t, repository, "extensions/plugins/com.vastplan.foundation.frontend.design-system.arco/vastplan.plugin.json")
	publishPortalFrontendPlugin(t, repository, "extensions/plugins/com.vastplan.foundation.frontend.composition.standard/vastplan.plugin.json")
	publishPortalFrontendPlugin(t, repository, "extensions/plugins/com.vastplan.foundation.frontend.layout.standard/vastplan.plugin.json")

	dir := t.TempDir()
	certFile, keyFile := writePortalTLSCertificate(t, dir)
	sessionFile := filepath.Join(dir, "sessions.json")
	writePortalSessions(t, sessionFile, map[string]portalSession{
		"author-token":    {ID: "author", Roles: []string{"portal.compose", "portal.approve"}},
		"approver-token":  {ID: "approver", Roles: []string{"portal.approve"}},
		"publisher-token": {ID: "publisher", Roles: []string{"portal.publish"}},
		"reader-token":    {ID: "reader", Roles: []string{"portal.read"}},
	})
	stateFile := filepath.Join(dir, "composer-state.json")
	interactionStateFile := filepath.Join(dir, "interaction-state.json")
	portalAssets := writePortalAssets(t, dir)
	portalCatalog := writePortalCatalogForTenant(t, dir, "acme")

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- portaledgecommand.Run(ctx, []string{
			"-listen", address,
			"-tls-cert", certFile,
			"-tls-key", keyFile,
			"-session-file", sessionFile,
			"-repository", repositoryRoot,
			"-install-root", filepath.Join(dir, "installed"),
			"-frontend-delivery-origin", filepath.Join(dir, "frontend-delivery-origin"),
			"-frontend-delivery-cache", filepath.Join(dir, "frontend-delivery-cache"),
			"-allow-unsigned-local",
			"-composer-version", "1.0.0",
			"-composer-state-file", stateFile,
			"-portal-platform-catalog", portalCatalog,
			"-interaction-broker-version", "0.1.0",
			"-interaction-broker-state-file", interactionStateFile,
			"-portal-assets", portalAssets,
		}, "0.1.0", func(format string, args ...any) { t.Logf("[portal-edge] "+format, args...) })
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("portal-edge shutdown: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("portal-edge did not stop")
		}
	})

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // #nosec G402 -- ephemeral E2E certificate.
	baseURL := "https://" + address
	waitForPortalEdge(t, client, baseURL)
	shellResponse, err := client.Get(baseURL + "/settings/portals")
	if err != nil {
		t.Fatal(err)
	}
	shellBody, err := io.ReadAll(shellResponse.Body)
	_ = shellResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if shellResponse.StatusCode != http.StatusOK || bytes.Contains(shellBody, []byte("__VASTPLAN_CSP_NONCE__")) || !strings.Contains(shellResponse.Header.Get("Content-Security-Policy"), "script-src 'self' blob: 'nonce-") {
		t.Fatalf("Portal Shell 未由 Edge 安全提供: status=%d headers=%v body=%s", shellResponse.StatusCode, shellResponse.Header, shellBody)
	}

	if status, _ := portalHTTPRequest(t, client, baseURL, "", "", http.MethodPost, "/v1/portal-drafts", portalSpec()); status != http.StatusUnauthorized {
		t.Fatalf("anonymous browser request status=%d, want %d", status, http.StatusUnauthorized)
	}
	if status, _ := portalHTTPRequest(t, client, baseURL, "unknown-token", "", http.MethodPost, "/v1/portal-drafts", portalSpec()); status != http.StatusUnauthorized {
		t.Fatalf("invalid browser session status=%d, want %d", status, http.StatusUnauthorized)
	}
	if status, _ := portalHTTPRequest(t, client, baseURL, "author-token", "", http.MethodPost, "/v1/portal-drafts", portalSpec()); status != http.StatusForbidden {
		t.Fatalf("missing CSRF token status=%d, want %d", status, http.StatusForbidden)
	}
	if status, _ := portalHTTPRequest(t, client, baseURL, "reader-token", portalCSRF(t, client, baseURL, "reader-token"), http.MethodPost, "/v1/portal-drafts", portalSpec()); status != http.StatusForbidden {
		t.Fatalf("read-only user must not create Portal draft: status=%d", status)
	}

	csrf := portalCSRF(t, client, baseURL, "author-token")
	status, raw := portalHTTPRequest(t, client, baseURL, "author-token", csrf, http.MethodPost, "/v1/portal-drafts", portalSpec())
	if status != http.StatusOK {
		t.Fatalf("create portal draft status=%d body=%s", status, raw)
	}
	var draft portalapi.Revision
	if err := json.Unmarshal(raw, &draft); err != nil {
		t.Fatal(err)
	}
	if draft.ID == 0 || draft.Status != portalapi.StatusDraft {
		t.Fatalf("unexpected created revision: %+v", draft)
	}
	updatedComposition := portalSpec()
	updatedComposition.Branding = map[string]any{"title": "Operations Portal"}
	csrf = portalCSRF(t, client, baseURL, "author-token")
	status, raw = portalHTTPRequest(t, client, baseURL, "author-token", csrf, http.MethodPut, fmt.Sprintf("/v1/portal-drafts/%d", draft.ID), updatedComposition)
	if status != http.StatusOK {
		t.Fatalf("update portal draft status=%d body=%s", status, raw)
	}
	var updated portalapi.Revision
	if err := json.Unmarshal(raw, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Composition.Branding["title"] != "Operations Portal" {
		t.Fatalf("updated composition was not persisted: %+v", updated.Composition)
	}

	csrf = portalCSRF(t, client, baseURL, "author-token")
	if status, raw = portalHTTPRequest(t, client, baseURL, "author-token", csrf, http.MethodPost, portalRevisionPath(draft.ID, "submit"), map[string]any{}); status != http.StatusOK {
		t.Fatalf("submit portal draft status=%d body=%s", status, raw)
	}
	csrf = portalCSRF(t, client, baseURL, "author-token")
	if status, _ := portalHTTPRequest(t, client, baseURL, "author-token", csrf, http.MethodPost, portalRevisionPath(draft.ID, "approve"), map[string]any{}); status != http.StatusForbidden {
		t.Fatalf("author must not approve own draft: status=%d", status)
	}
	csrf = portalCSRF(t, client, baseURL, "approver-token")
	if status, raw = portalHTTPRequest(t, client, baseURL, "approver-token", csrf, http.MethodPost, portalRevisionPath(draft.ID, "approve"), map[string]any{}); status != http.StatusOK {
		t.Fatalf("approve portal draft status=%d body=%s", status, raw)
	}
	csrf = portalCSRF(t, client, baseURL, "publisher-token")
	if status, raw = portalHTTPRequest(t, client, baseURL, "publisher-token", csrf, http.MethodPost, portalRevisionPath(draft.ID, "publish"), map[string]any{}); status != http.StatusOK {
		t.Fatalf("publish portal draft status=%d body=%s", status, raw)
	}
	var published portalapi.Revision
	if err := json.Unmarshal(raw, &published); err != nil {
		t.Fatal(err)
	}
	if published.Status != portalapi.StatusPublished || !published.Active {
		t.Fatalf("unexpected published revision: %+v", published)
	}
	status, raw = portalHTTPRequest(t, client, baseURL, "reader-token", "", http.MethodGet, "/v1/portal-runtime?path=/operations", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("read governed Portal runtime status=%d body=%s", status, raw)
	}
	var runtime portalapi.RuntimeSpec
	if err := json.Unmarshal(raw, &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.Portal.Revision != published.ID || len(runtime.Modules) != 8 || runtime.Modules[0].ID != "com.vastplan.foundation.frontend.design-system.arco" || runtime.Modules[1].ID != "com.vastplan.foundation.frontend.composition.standard" || runtime.Modules[2].ID != "com.vastplan.foundation.frontend.layout.standard" || runtime.Modules[3].ID != "com.vastplan.platform.configuration.portal-composer" || runtime.Modules[7].ID != "com.vastplan.platform.artifacts.repository" {
		t.Fatalf("unexpected governed runtime: %+v", runtime)
	}
	status, raw = portalHTTPRequest(t, client, baseURL, "reader-token", "", http.MethodGet, runtime.Modules[0].URL, map[string]any{})
	if status != http.StatusOK || !bytes.Contains(raw, []byte("ui.design-system")) {
		t.Fatalf("read verified frontend module status=%d body=%s", status, raw)
	}
	// Publish a newer revision first, which makes the first one a genuine
	// historical rollback candidate rather than allowing a no-op self rollback.
	csrf = portalCSRF(t, client, baseURL, "author-token")
	status, raw = portalHTTPRequest(t, client, baseURL, "author-token", csrf, http.MethodPost, "/v1/portal-drafts", portalSpec())
	if status != http.StatusOK {
		t.Fatalf("create newer portal draft status=%d body=%s", status, raw)
	}
	var newer portalapi.Revision
	if err := json.Unmarshal(raw, &newer); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		token     string
		operation string
	}{
		{token: "author-token", operation: "submit"},
		{token: "approver-token", operation: "approve"},
		{token: "publisher-token", operation: "publish"},
	} {
		csrf = portalCSRF(t, client, baseURL, step.token)
		if status, raw = portalHTTPRequest(t, client, baseURL, step.token, csrf, http.MethodPost, portalRevisionPath(newer.ID, step.operation), map[string]any{}); status != http.StatusOK {
			t.Fatalf("%s newer portal draft status=%d body=%s", step.operation, status, raw)
		}
	}
	status, raw = portalHTTPRequest(t, client, baseURL, "reader-token", "", http.MethodGet, "/v1/portal-recovery?path=/operations", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("read governed recovery runtime status=%d body=%s", status, raw)
	}
	var recovery portalapi.RuntimeSpec
	if err := json.Unmarshal(raw, &recovery); err != nil {
		t.Fatal(err)
	}
	if recovery.Portal.Revision != draft.ID || len(recovery.Modules) == 0 || !strings.HasPrefix(recovery.Modules[0].URL, fmt.Sprintf("/v1/portal-recovery-modules/%d/%d/", newer.ID, draft.ID)) {
		t.Fatalf("unexpected governed recovery runtime: %+v", recovery)
	}
	status, raw = portalHTTPRequest(t, client, baseURL, "reader-token", "", http.MethodGet, recovery.Modules[0].URL, map[string]any{})
	if status != http.StatusOK || !bytes.Contains(raw, []byte("ui.design-system")) {
		t.Fatalf("read verified recovery module status=%d body=%s", status, raw)
	}
	csrf = portalCSRF(t, client, baseURL, "publisher-token")
	status, raw = portalHTTPRequest(t, client, baseURL, "publisher-token", csrf, http.MethodPost, portalRevisionPath(draft.ID, "rollback"), map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("rollback historical revision status=%d body=%s", status, raw)
	}
	var rolledBack portalapi.Revision
	if err := json.Unmarshal(raw, &rolledBack); err != nil {
		t.Fatal(err)
	}
	if rolledBack.ID == newer.ID || rolledBack.Status != portalapi.StatusPublished || !rolledBack.Active {
		t.Fatalf("unexpected rollback revision: %+v", rolledBack)
	}
	status, raw = portalHTTPRequest(t, client, baseURL, "publisher-token", "", http.MethodGet, portalRevisionPath(rolledBack.ID, "audit"), map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("read rollback audit status=%d body=%s", status, raw)
	}
	var audit []portalapi.AuditEvent
	if err := json.Unmarshal(raw, &audit); err != nil {
		t.Fatal(err)
	}
	if len(audit) != 1 || audit[0].Action != "revision.rolled_back" {
		t.Fatalf("rollback audit missing: %+v", audit)
	}
}

func writePortalCatalogForTenant(t *testing.T, dir, tenantID string) string {
	t.Helper()
	catalog, err := frontendcompositionv1.ParsePortalPlatformCatalogFile(filepath.Join(repoRoot(t), "engineering", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range catalog.Bindings {
		catalog.Bindings[i].TenantID = tenantID
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "portal-platform-catalog.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type portalSession struct {
	ID    string
	Roles []string
}

func publishPortalFrontendPlugin(t *testing.T, repository *pluginservice.Repository, manifestPath string) {
	t.Helper()
	manifestRaw, err := os.ReadFile(filepath.Join(repoRoot(t), manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), manifestRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	entry := manifest.Entry["frontend"]
	if entry == "" {
		t.Fatal("frontend fixture manifest has no frontend entry")
	}
	entryPath := filepath.Join(dir, filepath.FromSlash(entry))
	if err := os.MkdirAll(filepath.Dir(entryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryPath, []byte(`export default { id: "ui.design-system", framework: "fixture", uiContract: "2.0.0", capabilities: ["layout","menu","overlay","form","data","feedback","theme"], Provider: ({children}) => children };`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{manifest.LicenseFile, manifest.NoticeFile} {
		if filename == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(repoRoot(t), filename))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(filename)), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pkg, _, err := pluginservice.PackageDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Publish("stable", pkg); err != nil {
		t.Fatal(err)
	}
}

func writePortalAssets(t *testing.T, parent string) string {
	t.Helper()
	root := filepath.Join(parent, "portal-assets")
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `<script type="importmap" nonce="__VASTPLAN_CSP_NONCE__">{"imports":{}}</script><script type="module" nonce="__VASTPLAN_CSP_NONCE__" src="/assets/portal.js"></script>`
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "portal.js"), []byte("export {};"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func writePortalSessions(t *testing.T, filename string, sessions map[string]portalSession) {
	t.Helper()
	type record struct {
		TokenSHA256 string   `json:"tokenSHA256"`
		ID          string   `json:"id"`
		TenantID    string   `json:"tenantId"`
		Roles       []string `json:"roles"`
		ExpiresAt   string   `json:"expiresAt"`
	}
	doc := struct {
		Sessions []record `json:"sessions"`
	}{}
	for token, session := range sessions {
		digest := sha256.Sum256([]byte(token))
		doc.Sessions = append(doc.Sessions, record{TokenSHA256: hex.EncodeToString(digest[:]), ID: session.ID, TenantID: "acme", Roles: session.Roles, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writePortalTLSCertificate(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "portal-edge-e2e"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

func waitForPortalEdge(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(baseURL + "/v1/csrf")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("portal-edge did not become ready")
}

func portalCSRF(t *testing.T, client *http.Client, baseURL, session string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, baseURL+"/v1/csrf", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: "vastplan_session", Value: session})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CSRF status=%d", response.StatusCode)
	}
	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || result.Token == "" {
		t.Fatalf("invalid CSRF response: %v", err)
	}
	return result.Token
}

func portalHTTPRequest(t *testing.T, client *http.Client, baseURL, session, csrf, method, path string, payload any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(method, baseURL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if session != "" {
		request.AddCookie(&http.Cookie{Name: "vastplan_session", Value: session})
	}
	if csrf != "" {
		request.AddCookie(&http.Cookie{Name: "vastplan_csrf", Value: csrf})
		request.Header.Set("X-VastPlan-CSRF", csrf)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	result, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, result
}

func portalRevisionPath(id uint64, operation string) string {
	return fmt.Sprintf("/v1/portal-drafts/%d/%s", id, operation)
}

func portalSpec() frontendcompositionv1.ApplicationComposition {
	return frontendcompositionv1.ApplicationComposition{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "operations"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend}, Route: "/operations", Plugins: []frontendcompositionv1.PluginRef{}}
}
