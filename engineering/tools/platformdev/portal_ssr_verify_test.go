package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyPortalColdStartAcceptsRenderedDeclarativeShadowDOM(t *testing.T) {
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
	if err := verifyPortalColdStart(server.Client(), server.URL, devAdminToken); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPortalColdStartAcceptsExplicitCSRBypass(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-VastPlan-SSR", "bypass")
		_, _ = response.Write([]byte(`<div id="vastplan-portal" aria-live="polite"></div>`))
	}))
	defer server.Close()
	if err := verifyPortalColdStart(server.Client(), server.URL, devAdminToken); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyPortalColdStartRejectsSSRFailureFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-VastPlan-SSR", "fallback")
		_, _ = response.Write([]byte(`<div id="vastplan-portal"></div>`))
	}))
	defer server.Close()
	if err := verifyPortalColdStart(server.Client(), server.URL, devAdminToken); err == nil {
		t.Fatal("平台启动不得把 SSR 执行失败误报为就绪")
	}
}
