package main

import (
	"errors"
	"net/http"
	"path/filepath"
)

// developmentIdentityProtocol is selected once by the development composition
// root. Gateway, HMR and Portal startup consume the selected policy without
// interpreting environment or mode strings again.
type developmentIdentityProtocol interface {
	name() string
	prepare(*runtime) error
	portalArguments(*runtime) []string
	decorateUpstream(source, target *http.Request)
	needsBootstrapPublisher() bool
}

func selectDevelopmentIdentityProtocol(autoLogin bool) developmentIdentityProtocol {
	if autoLogin {
		return autoLoginIdentityProtocol{}
	}
	return brokerIdentityProtocol{}
}

type brokerIdentityProtocol struct{}

func (brokerIdentityProtocol) name() string { return "broker" }

func (brokerIdentityProtocol) prepare(r *runtime) error {
	return r.ensureDevelopmentAuthenticationMaterial()
}

func (brokerIdentityProtocol) portalArguments(r *runtime) []string {
	return []string{
		"--identity-provider", "broker",
		"--authentication-assertion-trust-file", r.authenticationAssertionTrustPath(),
		"--portal-session-key-file", r.portalSessionKeyPath(),
		"--authentication-broker-logical-service", "foundation.security.authentication-broker",
		"--authorization-session-logical-service", "foundation.security.authorization-session",
	}
}

func (brokerIdentityProtocol) decorateUpstream(source, target *http.Request) {
	copyIdentityCookies(source, target)
}

func (brokerIdentityProtocol) needsBootstrapPublisher() bool { return true }

type autoLoginIdentityProtocol struct{}

func (autoLoginIdentityProtocol) name() string { return "auto-login" }
func (autoLoginIdentityProtocol) prepare(r *runtime) error {
	// Auto-login replaces only the browser-facing identity protocol. The Seed
	// still starts Authentication Broker as a bootstrap service, so its signed
	// assertion material must exist exactly as in interactive mode.
	return r.ensureDevelopmentAuthenticationMaterial()
}
func (autoLoginIdentityProtocol) portalArguments(r *runtime) []string {
	return []string{"--identity-provider", "file", "--session-file", filepath.Join(r.runDir, "secrets", "portal-sessions.json")}
}
func (autoLoginIdentityProtocol) decorateUpstream(source, target *http.Request) {
	copyIdentityCookies(source, target)
	if _, err := target.Cookie("vastplan_session"); errors.Is(err, http.ErrNoCookie) {
		target.AddCookie(&http.Cookie{Name: "vastplan_session", Value: devAdminToken})
	}
}
func (autoLoginIdentityProtocol) needsBootstrapPublisher() bool { return false }

func copyIdentityCookies(source, target *http.Request) {
	if source == nil || target == nil {
		return
	}
	allowed := map[string]struct{}{
		"vastplan_session": {}, "vastplan_auth_proof": {}, "vastplan_auth_tx": {},
		"vastplan_auth_test": {}, "vastplan_csrf": {},
	}
	target.Header.Del("Cookie")
	for _, cookie := range source.Cookies() {
		if _, ok := allowed[cookie.Name]; ok {
			target.AddCookie(cookie)
		}
	}
}
