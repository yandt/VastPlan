package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestRuntimeDescriptorMatchesSignedManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(expected) != 1 {
		t.Fatalf("manifest contributions invalid: %+v err=%v", expected, err)
	}
	var left, right any
	if json.Unmarshal(expected[0].Descriptor, &left) != nil || json.Unmarshal(runtimeRepositoryDescriptor, &right) != nil {
		t.Fatal("descriptors must be JSON")
	}
	leftRaw, _ := json.Marshal(left)
	rightRaw, _ := json.Marshal(right)
	if !bytes.Equal(leftRaw, rightRaw) {
		t.Fatalf("runtime descriptor differs from signed manifest:\nwant=%s\ngot=%s", leftRaw, rightRaw)
	}
}

func TestReferenceOwnerAuthorityIsNarrow(t *testing.T) {
	if !referenceOwnerAllowed("cn.vastplan.platform.infrastructure.deployment-manager", "deployment-active") || !referenceOwnerAllowed("cn.vastplan.platform.configuration.portal-composer", "portal-activation") {
		t.Fatal("trusted controllers must publish only their owned reference classes")
	}
	if referenceOwnerAllowed("cn.example.business", "deployment-active") || referenceOwnerAllowed("cn.vastplan.platform.configuration.portal-composer", "assignment-active") {
		t.Fatal("business or cross-domain owner claims must be denied")
	}
}

func TestLoadConfigRequiresDistinctCompleteConfiguration(t *testing.T) {
	t.Setenv("VASTPLAN_PLUGIN_CONFIG_JSON", `{}`)
	t.Setenv("VASTPLAN_ARTIFACT_REPOSITORY", "")
	t.Setenv("VASTPLAN_ARTIFACT_TRUST", "")
	t.Setenv("VASTPLAN_ARTIFACT_TLS_CERT", "")
	t.Setenv("VASTPLAN_ARTIFACT_TLS_KEY", "")
	t.Setenv("VASTPLAN_ARTIFACT_READ_TOKEN", "")
	t.Setenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN", "")
	t.Setenv("VASTPLAN_ARTIFACT_BUNDLE_TOKEN", "")
	t.Setenv("VASTPLAN_ARTIFACT_MIGRATION_STATE", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("incomplete artifact repository configuration must fail closed")
	}

	t.Setenv("VASTPLAN_ARTIFACT_REPOSITORY", "/var/lib/vastplan/artifacts")
	t.Setenv("VASTPLAN_ARTIFACT_TRUST", "/etc/vastplan/trust.json")
	t.Setenv("VASTPLAN_ARTIFACT_TLS_CERT", "/etc/vastplan/tls.crt")
	t.Setenv("VASTPLAN_ARTIFACT_TLS_KEY", "/etc/vastplan/tls.key")
	t.Setenv("VASTPLAN_ARTIFACT_READ_TOKEN", "shared")
	t.Setenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN", "shared")
	t.Setenv("VASTPLAN_ARTIFACT_BUNDLE_TOKEN", "bundle")
	t.Setenv("VASTPLAN_ARTIFACT_MIGRATION_STATE", "/var/lib/vastplan/control/repository-migration.json")
	if _, err := loadConfig(); err == nil {
		t.Fatal("read and publish tokens must be separated")

	}

	t.Setenv("VASTPLAN_ARTIFACT_READ_TOKEN", "reader")
	t.Setenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN", "publisher")
	t.Setenv("VASTPLAN_ARTIFACT_BUNDLE_TOKEN", "bundle")
	config, err := loadConfig()
	if err != nil {
		t.Fatalf("complete distinct configuration rejected: %v", err)
	}
	if config.addr != "127.0.0.1:8443" {
		t.Fatalf("default listen address = %q", config.addr)
	}
	if config.storageProvider != "platform.artifacts.storage.file" {
		t.Fatalf("default storage provider = %q", config.storageProvider)
	}
	if config.volumeID != "repository.primary" {
		t.Fatalf("default storage volume = %q", config.volumeID)
	}
	t.Setenv("VASTPLAN_PLUGIN_CONFIG_JSON", `{"storageProvider":"../../escape"}`)
	if _, err := loadConfig(); err == nil {
		t.Fatal("invalid storage provider id must fail closed")
	}
}
