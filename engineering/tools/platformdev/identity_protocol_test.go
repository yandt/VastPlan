package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	broker "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authentication-broker/broker"
	seedaccess "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.seed-access/seedaccess"
)

func TestDevelopmentIdentityProtocolDefaultsToBrokerWithoutInjection(t *testing.T) {
	protocol := selectDevelopmentIdentityProtocol(false)
	if protocol.name() != "broker" || !protocol.needsBootstrapPublisher() {
		t.Fatalf("unexpected default protocol: %T %q", protocol, protocol.name())
	}
	source := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/operations", nil)
	target := httptest.NewRequest(http.MethodGet, "https://127.0.0.1/operations", nil)
	protocol.decorateUpstream(source, target)
	if _, err := target.Cookie("vastplan_session"); err != http.ErrNoCookie {
		t.Fatalf("Broker protocol must not inject a session: %v", err)
	}
	source.AddCookie(&http.Cookie{Name: "vastplan_session", Value: "browser-session"})
	source.AddCookie(&http.Cookie{Name: "vastplan_auth_proof", Value: "authentication-proof"})
	source.AddCookie(&http.Cookie{Name: "unrelated", Value: "must-not-cross-boundary"})
	target = httptest.NewRequest(http.MethodGet, "https://127.0.0.1/operations", nil)
	target.AddCookie(&http.Cookie{Name: "vastplan_session", Value: "stale-session"})
	protocol.decorateUpstream(source, target)
	if cookie, err := target.Cookie("vastplan_session"); err != nil || cookie.Value != "browser-session" {
		t.Fatalf("Broker protocol did not forward browser session: cookie=%v err=%v", cookie, err)
	}
	if cookie, err := target.Cookie("vastplan_auth_proof"); err != nil || cookie.Value != "authentication-proof" {
		t.Fatalf("Broker protocol did not forward proof: cookie=%v err=%v", cookie, err)
	}
	if _, err := target.Cookie("unrelated"); err != http.ErrNoCookie {
		t.Fatalf("Broker protocol forwarded an unrelated cookie: %v", err)
	}
}

func TestAutoLoginProtocolIsExplicitAndPreservesBrowserSession(t *testing.T) {
	protocol := selectDevelopmentIdentityProtocol(true)
	if protocol.name() != "auto-login" || protocol.needsBootstrapPublisher() {
		t.Fatalf("unexpected auto-login protocol: %T %q", protocol, protocol.name())
	}
	source := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/operations", nil)
	target := httptest.NewRequest(http.MethodGet, "https://127.0.0.1/operations", nil)
	protocol.decorateUpstream(source, target)
	if cookie, err := target.Cookie("vastplan_session"); err != nil || cookie.Value != devAdminToken {
		t.Fatalf("auto-login did not inject development owner: cookie=%v err=%v", cookie, err)
	}
	source.AddCookie(&http.Cookie{Name: "vastplan_session", Value: "browser-session"})
	target = httptest.NewRequest(http.MethodGet, "https://127.0.0.1/operations", nil)
	protocol.decorateUpstream(source, target)
	if cookie, err := target.Cookie("vastplan_session"); err != nil || cookie.Value != "browser-session" {
		t.Fatalf("auto-login overwrote browser session: cookie=%v err=%v", cookie, err)
	}
}

func TestDevelopmentAuthenticationMaterialAndSeedSubject(t *testing.T) {
	root := t.TempDir()
	r := &runtime{options: options{stateRoot: root}, runDir: filepath.Join(root, "run")}
	if err := os.MkdirAll(filepath.Join(root, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := (brokerIdentityProtocol{}).prepare(r); err != nil {
		t.Fatal(err)
	}
	state, err := (&broker.FileManagementStore{Path: r.authenticationProviderStatePath()}).LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Catalog == nil {
		t.Fatal("development provider catalog was not created")
	}
	provider, ok := state.Catalog.Resolve("local", "operations", "seed-password")
	if !ok || provider.ContributionID != "seed-local" || provider.Profile.ID != developmentSeedProviderProfileID {
		t.Fatalf("unexpected seed provider route: %+v found=%v", provider, ok)
	}
	if info, err := os.Lstat(r.portalSessionKeyPath()); err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("portal session key permissions are unsafe: info=%v err=%v", info, err)
	}
	authority, err := seedaccess.NewAuthority(seedaccess.FileStore{Path: r.seedAccessStatePath()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Initialize("owner", []byte("local-development-password")); err != nil {
		t.Fatal(err)
	}
	actual, err := r.developmentSeedSubjectID()
	if err != nil {
		t.Fatal(err)
	}
	expected := authenticationv1.StableSubjectID(developmentSeedProviderProfileID, developmentSeedIssuer, "owner")
	if actual != expected {
		t.Fatalf("stable Seed subject mismatch: got=%s want=%s", actual, expected)
	}
}
