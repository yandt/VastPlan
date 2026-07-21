//go:build e2e

package e2e

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type portalOIDCProvider struct {
	issuer   string
	clientID string
	key      *rsa.PrivateKey
	server   *httptest.Server
	mu       sync.Mutex
	codes    map[string]portalOIDCCode
	next     portalOIDCIdentity
}

type portalOIDCCode struct {
	nonce, challenge, redirectURI string
	identity                      portalOIDCIdentity
}

type portalOIDCIdentity struct {
	sub, tenant string
	roles       []string
}

func startPortalOIDCProvider(t *testing.T, clientID string) *portalOIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	provider := &portalOIDCProvider{
		clientID: clientID, key: key, codes: map[string]portalOIDCCode{},
		next: portalOIDCIdentity{sub: "alice", tenant: "acme", roles: []string{"portal.read"}},
	}
	provider.server = httptest.NewServer(http.HandlerFunc(provider.handle))
	provider.issuer = provider.server.URL + "/"
	t.Cleanup(provider.server.Close)
	return provider
}

func (p *portalOIDCProvider) handle(response http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/.well-known/openid-configuration":
		p.writeJSON(response, http.StatusOK, map[string]any{
			"issuer": p.issuer, "authorization_endpoint": p.issuer + "authorize", "token_endpoint": p.issuer + "token", "jwks_uri": p.issuer + "jwks",
			"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"},
			"subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"},
			"token_endpoint_auth_methods_supported": []string{"none"}, "code_challenge_methods_supported": []string{"S256"},
		})
	case "/jwks":
		p.writeJSON(response, http.StatusOK, map[string]any{"keys": []any{p.publicJWK()}})
	case "/authorize":
		p.authorize(response, request)
	case "/token":
		p.token(response, request)
	default:
		p.writeJSON(response, http.StatusNotFound, map[string]string{"error": "not_found"})
	}
}

func (p *portalOIDCProvider) authorize(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	state, nonce := query.Get("state"), query.Get("nonce")
	challenge, redirectURI := query.Get("code_challenge"), query.Get("redirect_uri")
	if query.Get("client_id") != p.clientID || query.Get("response_type") != "code" || query.Get("code_challenge_method") != "S256" || state == "" || nonce == "" || challenge == "" || redirectURI == "" {
		p.writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	code := randomBase64URL(24)
	p.mu.Lock()
	p.codes[code] = portalOIDCCode{nonce: nonce, challenge: challenge, redirectURI: redirectURI, identity: clonePortalOIDCIdentity(p.next)}
	p.mu.Unlock()
	http.Redirect(response, request, redirectURI+"?code="+code+"&state="+state, http.StatusFound)
}

func (p *portalOIDCProvider) token(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.ParseForm() != nil {
		p.writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	code, verifier := request.Form.Get("code"), request.Form.Get("code_verifier")
	p.mu.Lock()
	transaction, exists := p.codes[code]
	if exists {
		delete(p.codes, code)
	}
	p.mu.Unlock()
	digest := sha256.Sum256([]byte(verifier))
	if !exists || request.Form.Get("grant_type") != "authorization_code" || request.Form.Get("client_id") != p.clientID ||
		request.Form.Get("redirect_uri") != transaction.redirectURI || base64.RawURLEncoding.EncodeToString(digest[:]) != transaction.challenge {
		p.writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now().Unix()
	idToken, err := p.signJWT(map[string]any{
		"iss": p.issuer, "aud": p.clientID, "sub": transaction.identity.sub, "iat": now, "exp": now + 600,
		"nonce": transaction.nonce, "tenant_id": transaction.identity.tenant, "roles": transaction.identity.roles,
	})
	if err != nil {
		p.writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	p.writeJSON(response, http.StatusOK, map[string]any{
		"access_token": "must-not-reach-browser", "token_type": "Bearer", "expires_in": 300, "id_token": idToken,
	})
}

func (p *portalOIDCProvider) selectIdentity(subject, tenant string, roles ...string) {
	p.mu.Lock()
	p.next = portalOIDCIdentity{sub: subject, tenant: tenant, roles: append([]string(nil), roles...)}
	p.mu.Unlock()
}

func clonePortalOIDCIdentity(identity portalOIDCIdentity) portalOIDCIdentity {
	identity.roles = append([]string(nil), identity.roles...)
	return identity
}

func (p *portalOIDCProvider) publicJWK() map[string]string {
	return map[string]string{
		"kty": "RSA", "kid": "portal-e2e", "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(p.key.PublicKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.key.PublicKey.E)).Bytes()),
	}
}

func (p *portalOIDCProvider) signJWT(claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": "portal-e2e", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (p *portalOIDCProvider) writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		panic(fmt.Sprintf("write OIDC fixture response: %v", err))
	}
}

func randomBase64URL(size int) string {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
