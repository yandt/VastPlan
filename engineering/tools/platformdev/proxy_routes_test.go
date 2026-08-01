package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPortalKernelRoutesIncludeGovernedAPIExposure(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"/v1":                                true,
		"/v1/portal-runtime":                 true,
		"/auth":                              true,
		"/auth/v1/bootstrap":                 true,
		"/api":                               true,
		"/api/r/aaaaaaaaaaaaaaaaaaaa/v1/x":   true,
		"/api/d/bbbbbbbbbbbbbbbbbbbb/ticket": true,
		"/apix":                              false,
		"/assets/portal.css":                 false,
		"/operations":                        false,
	}
	for path, expected := range tests {
		path, expected := path, expected
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			if actual := isPortalKernelRoute(path); actual != expected {
				t.Fatalf("isPortalKernelRoute(%q) = %v, want %v", path, actual, expected)
			}
		})
	}
}

func TestDevelopmentPortalProxyCanonicalizesRootBeforeLogin(t *testing.T) {
	t.Parallel()

	forwarded := 0
	handler := developmentPortalProxy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded++
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		request := httptest.NewRequest(method, "http://127.0.0.1/?ignored=true", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusTemporaryRedirect || response.Header().Get("Location") != "/operations" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s root response = %d headers=%v", method, response.Code, response.Header())
		}
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/operations", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || forwarded != 1 {
		t.Fatalf("governed Portal path must reach upstream: code=%d forwarded=%d", response.Code, forwarded)
	}
}
