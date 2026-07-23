package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverPackageSpecsIncludesFrontendManifestWithoutAllowList(t *testing.T) {
	root := t.TempDir()
	pluginID := "cn.vastplan.example.frontend"
	pluginRoot := filepath.Join(root, "extensions", "plugins", pluginID)
	if err := os.MkdirAll(pluginRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	reference := filepath.Join(repositoryRoot(t), "extensions", "plugins", "cn.vastplan.foundation.frontend.workflow.workbench", "vastplan.plugin.json")
	raw, err := os.ReadFile(reference)
	if err != nil {
		t.Fatal(err)
	}
	manifest := strings.Replace(string(raw), "cn.vastplan.foundation.frontend.workflow.workbench", pluginID, 1)
	if err := os.WriteFile(filepath.Join(pluginRoot, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	specs, err := discoverPackageSpecs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].id != pluginID || !specs[0].frontend || specs[0].backend || specs[0].frontendEntry != "frontend/dist/index.js" {
		t.Fatalf("discovered specs = %#v", specs)
	}
}

func TestDiscoverPackageSpecsIncludesNodeBackendWithoutNativeBinary(t *testing.T) {
	root := t.TempDir()
	pluginID := "cn.vastplan.platform.security.authentication.delivery.webhook"
	pluginRoot := filepath.Join(root, "extensions", "plugins", pluginID)
	if err := os.MkdirAll(pluginRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	reference := filepath.Join(repositoryRoot(t), "extensions", "plugins", pluginID, "vastplan.plugin.json")
	raw, err := os.ReadFile(reference)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, "vastplan.plugin.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	specs, err := discoverPackageSpecs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].id != pluginID || specs[0].backend || specs[0].frontend {
		t.Fatalf("Node backend must be source-packaged without a native binary: %#v", specs)
	}
}

func TestDiscoverPackageSpecsLeavesDynamicGoToDedicatedPackager(t *testing.T) {
	specs, err := discoverPackageSpecs(repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		if spec.id == "cn.vastplan.foundation.security.bootstrap-policy" {
			t.Fatalf("dynamic-go plugin must not be packaged as a native backend: %#v", spec)
		}
	}
}

func TestPluginManifestVersionUsesManifestAsSourceOfTruth(t *testing.T) {
	version, err := pluginManifestVersion(repositoryRoot(t), "cn.vastplan.platform.configuration.portal-composer")
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.6.0" {
		t.Fatalf("version = %q, want 1.6.0", version)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "extensions", "plugins")); err != nil {
		t.Fatalf("repository root %s: %v", root, err)
	}
	return root
}
