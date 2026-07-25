package artifactreport

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestArchivePublishesAndReusesCompleteContentAddressedReport(t *testing.T) {
	root := filepath.Join(t.TempDir(), "reports")
	archive, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"SchemaVersion":2,"Results":[]}`)
	digest := sha256.Sum256(raw)
	want := hex.EncodeToString(digest[:])
	if err := archive.Put(want, raw); err != nil {
		t.Fatal(err)
	}
	if err := archive.Put(want, raw); err != nil {
		t.Fatalf("相同摘要归档必须幂等: %v", err)
	}
	if err := archive.Require(want); err != nil {
		t.Fatal(err)
	}
	loaded, err := archive.Read(want)
	if err != nil || string(loaded) != string(raw) {
		t.Fatalf("读取归档报告失败: raw=%s err=%v", loaded, err)
	}
	info, err := os.Lstat(filepath.Join(root, want+".json"))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("归档文件权限无效: info=%v err=%v", info, err)
	}
}

func TestArchiveRejectsTamperSymlinkAndPublicRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "reports")
	archive, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("report")
	digest := sha256.Sum256(raw)
	want := hex.EncodeToString(digest[:])
	if err := os.WriteFile(filepath.Join(root, want+".json"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := archive.Require(want); err == nil {
		t.Fatal("内容篡改必须拒绝")
	}
	if err := os.Remove(filepath.Join(root, want+".json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside"), filepath.Join(root, want+".json")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Require(want); err == nil {
		t.Fatal("符号链接报告必须拒绝")
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := archive.Require(want); err == nil {
		t.Fatal("group/other 可访问的归档根必须拒绝")
	}
}
