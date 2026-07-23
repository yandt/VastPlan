package main

import (
	"os"
	"path/filepath"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstatebackup"
)

func TestKeygenCreatesNonOverwritingPrivateKeyAndTrust(t *testing.T) {
	root := t.TempDir()
	privatePath := filepath.Join(root, "keys", "backup.pem")
	trustPath := filepath.Join(root, "trust", "backup.json")
	arguments := []string{"-key-id", "backup-1", "-private-out", privatePath, "-trust-out", trustPath}
	if err := runKeygen(arguments); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode=%v", info.Mode())
	}
	if _, err := sharedstatebackup.LoadPrivateKeyPEM(privatePath); err != nil {
		t.Fatal(err)
	}
	if _, err := sharedstatebackup.LoadTrustDocument(trustPath); err != nil {
		t.Fatal(err)
	}
	if err := runKeygen(arguments); err == nil {
		t.Fatal("keygen 不得覆盖已有密钥")
	}
}

func TestCapacityAlertLevelsAreClosedAndOrdered(t *testing.T) {
	for _, value := range []string{"none", "warning", "critical", "full"} {
		if _, err := parseCapacityLevel(value); err != nil {
			t.Fatalf("合法 fail-on 被拒绝 %s: %v", value, err)
		}
	}
	if _, err := parseCapacityLevel("tenant-a"); err == nil {
		t.Fatal("未知或业务化告警级别不得接受")
	}
	if !(capacityRank(sharedstate.CapacityReady) < capacityRank(sharedstate.CapacityWarning) &&
		capacityRank(sharedstate.CapacityWarning) < capacityRank(sharedstate.CapacityCritical) &&
		capacityRank(sharedstate.CapacityCritical) < capacityRank(sharedstate.CapacityFull)) {
		t.Fatal("容量级别顺序错误")
	}
}
