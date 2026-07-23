package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExistingPackagePreservesExactCandidateBytes(t *testing.T) {
	raw := packageForRemotePublish(t, "candidate")
	filename := filepath.Join(t.TempDir(), "candidate.tar.gz")
	if err := os.WriteFile(filename, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, id, version, publisher, err := loadExistingPackage(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != string(raw) || id != "cn.vastplan.product.release-test" || version != "1.0.0" || publisher != "vastplan" {
		t.Fatalf("不可变候选没有原样复用: %s@%s publisher=%s", id, version, publisher)
	}
}
