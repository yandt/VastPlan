package edge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSessions(t *testing.T, path, token string, expiresAt time.Time) {
	t.Helper()
	digest := sha256.Sum256([]byte(token))
	raw, err := json.Marshal(sessionDocument{Sessions: []sessionRecord{{
		TokenSHA256: hex.EncodeToString(digest[:]), ID: "alice", TenantID: "tenant-a", Roles: []string{"portal.compose"}, ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFileIdentityProviderAcceptsDigestOnlySession(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "sessions.json")
	writeSessions(t, path, "opaque-browser-token", now.Add(time.Hour))
	p, err := NewFileIdentityProvider(path)
	if err != nil {
		t.Fatal(err)
	}
	p.now = func() time.Time { return now }
	req := httptest.NewRequest("GET", "https://portal.example/v1/portal-drafts", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "opaque-browser-token"})
	principal, err := p.Authenticate(req)
	if err != nil || principal.ID != "alice" || principal.TenantID != "tenant-a" {
		t.Fatalf("有效 session 未得到身份: principal=%+v err=%v", principal, err)
	}
}

func TestFileIdentityProviderRejectsExpiredDuplicateAndInsecureFile(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "sessions.json")
	writeSessions(t, path, "token", now.Add(-time.Second))
	p, _ := NewFileIdentityProvider(path)
	p.now = func() time.Time { return now }
	req := httptest.NewRequest("GET", "https://portal.example", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "token"})
	if _, err := p.Authenticate(req); err == nil {
		t.Fatal("过期 session 必须拒绝")
	}
	writeSessions(t, path, "token", now.Add(time.Hour))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "token-2"})
	if _, err := p.Authenticate(req); err == nil {
		t.Fatal("重复同名 cookie 必须拒绝")
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("GET", "https://portal.example", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "token"})
	if _, err := p.Authenticate(req); err == nil {
		t.Fatal("宽松权限 session 文件必须拒绝")
	}
}
