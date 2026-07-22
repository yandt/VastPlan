package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeAccessProfileCatalogBindsExistingPlatformProfile(t *testing.T) {
	root := filepath.Join("..", "..")
	target := filepath.Join(t.TempDir(), "access.json")
	if err := materializeAccessProfileCatalog(
		filepath.Join(root, "deploy", "portal-access-profile-catalog.json"),
		filepath.Join(root, "deploy", "portal-platform-catalog.json"), target,
	); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("Access Catalog 必须以 0600 物化: info=%v err=%v", info, err)
	}
}
