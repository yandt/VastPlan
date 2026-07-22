//go:build e2e

package e2e

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type portalFileIdentityFixture struct {
	mu       sync.Mutex
	path     string
	sessions []map[string]any
}

func startPortalFileIdentityFixture(t *testing.T) *portalFileIdentityFixture {
	t.Helper()
	fixture := &portalFileIdentityFixture{path: filepath.Join(t.TempDir(), "portal-sessions.json"), sessions: []map[string]any{}}
	fixture.write(t)
	return fixture
}

func (f *portalFileIdentityFixture) login(t *testing.T, process *portalKernelProcess, subject, tenant string, roles ...string) *http.Client {
	t.Helper()
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil { t.Fatal(err) }
	token := hex.EncodeToString(tokenBytes)
	digest := sha256.Sum256([]byte(token))
	f.mu.Lock()
	f.sessions = append(f.sessions, map[string]any{"tokenSHA256": hex.EncodeToString(digest[:]), "id": subject, "tenantId": tenant, "roles": roles, "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)})
	f.writeLocked(t)
	f.mu.Unlock()
	client := portalKernelBrowserClient(t)
	location, err := url.Parse(process.baseURL())
	if err != nil { t.Fatal(err) }
	client.Jar.SetCookies(location, []*http.Cookie{{Name: "vastplan_session", Value: token, Path: "/", Secure: true, HttpOnly: true}})
	return client
}

func (f *portalFileIdentityFixture) write(t *testing.T) { t.Helper(); f.mu.Lock(); defer f.mu.Unlock(); f.writeLocked(t) }
func (f *portalFileIdentityFixture) writeLocked(t *testing.T) { raw, err := json.Marshal(map[string]any{"sessions": f.sessions}); if err != nil { t.Fatal(err) }; if err := os.WriteFile(f.path, raw, 0o600); err != nil { t.Fatal(err) } }
