package main

import "testing"

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
