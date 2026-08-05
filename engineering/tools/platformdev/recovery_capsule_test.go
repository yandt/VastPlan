package main

import (
	"path/filepath"
	"testing"
)

func TestPlatformRecoveryPlanCoversProfileAndDerivesLKGPlugins(t *testing.T) {
	root := platformDevTestProjectRoot(t)
	runDir := t.TempDir()
	if err := copySnapshotFile(
		filepath.Join(root, "engineering", "deploy", "platform-management-profile.json"),
		filepath.Join(runDir, "platform-management-profile.json"),
	); err != nil {
		t.Fatal(err)
	}
	r := &runtime{options: options{root: root}, runDir: runDir}
	plugins, err := r.recoveryPluginIDs()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"cn.vastplan.foundation.data.relational.runtime",
		"cn.vastplan.foundation.security.authentication-broker",
		"cn.vastplan.foundation.security.authorization-enforcer",
		"cn.vastplan.foundation.security.authorization-session",
		"cn.vastplan.foundation.security.seed-access",
		"cn.vastplan.platform.data.relational.connection-manager",
	} {
		if _, ok := plugins[id]; !ok {
			t.Fatalf("Recovery LKG 缺少 %s: %v", id, plugins)
		}
	}
	if len(plugins) != 6 {
		t.Fatalf("Recovery LKG 不应隐式扩大: %v", plugins)
	}
}
