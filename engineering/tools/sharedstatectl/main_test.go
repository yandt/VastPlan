package main

import (
	"os"
	"path/filepath"
	"testing"

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
