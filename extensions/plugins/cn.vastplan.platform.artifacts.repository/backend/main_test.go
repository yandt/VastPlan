package main

import "testing"

func TestLoadConfigRequiresDistinctCompleteConfiguration(t *testing.T) {
	t.Setenv("VASTPLAN_PLUGIN_CONFIG_JSON", `{}`)
	t.Setenv("VASTPLAN_ARTIFACT_REPOSITORY", "")
	t.Setenv("VASTPLAN_ARTIFACT_TRUST", "")
	t.Setenv("VASTPLAN_ARTIFACT_TLS_CERT", "")
	t.Setenv("VASTPLAN_ARTIFACT_TLS_KEY", "")
	t.Setenv("VASTPLAN_ARTIFACT_READ_TOKEN", "")
	t.Setenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("incomplete artifact repository configuration must fail closed")
	}

	t.Setenv("VASTPLAN_ARTIFACT_REPOSITORY", "/var/lib/vastplan/artifacts")
	t.Setenv("VASTPLAN_ARTIFACT_TRUST", "/etc/vastplan/trust.json")
	t.Setenv("VASTPLAN_ARTIFACT_TLS_CERT", "/etc/vastplan/tls.crt")
	t.Setenv("VASTPLAN_ARTIFACT_TLS_KEY", "/etc/vastplan/tls.key")
	t.Setenv("VASTPLAN_ARTIFACT_READ_TOKEN", "shared")
	t.Setenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN", "shared")
	if _, err := loadConfig(); err == nil {
		t.Fatal("read and publish tokens must be separated")

	}

	t.Setenv("VASTPLAN_ARTIFACT_READ_TOKEN", "reader")
	t.Setenv("VASTPLAN_ARTIFACT_PUBLISH_TOKEN", "publisher")
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
	t.Setenv("VASTPLAN_PLUGIN_CONFIG_JSON", `{"storageProvider":"../../escape"}`)
	if _, err := loadConfig(); err == nil {
		t.Fatal("invalid storage provider id must fail closed")
	}
}
