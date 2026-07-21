package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyPortalSSRRequiresRenderedDeclarativeShadowDOM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie("vastplan_session")
		if err != nil || cookie.Value != devAdminToken {
			http.Error(response, "missing session", http.StatusUnauthorized)
			return
		}
		response.Header().Set("X-VastPlan-SSR", "rendered")
		_, _ = response.Write([]byte(`<div><template shadowrootmode="open"><main>ready</main></template></div>`))
	}))
	defer server.Close()
	if err := verifyPortalSSR(server.Client(), server.URL, devAdminToken); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPortalSSRRejectsCSRCompatibilityFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-VastPlan-SSR", "fallback")
		_, _ = response.Write([]byte(`<div id="vastplan-portal"></div>`))
	}))
	defer server.Close()
	if err := verifyPortalSSR(server.Client(), server.URL, devAdminToken); err == nil {
		t.Fatal("平台启动不得把 SSR fallback 误报为完整就绪")
	}
}
