package oidcmanifest_test

import (
	"os"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestManifest(t *testing.T) {
	raw, err := os.ReadFile("vastplan.plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(contributions) != 1 || contributions[0].ExtensionPoint != "authentication.provider" || contributions[0].ID != "enterprise-oidc" {
		t.Fatalf("OIDC Provider Manifest 无效: %+v %v", contributions, err)
	}
}
