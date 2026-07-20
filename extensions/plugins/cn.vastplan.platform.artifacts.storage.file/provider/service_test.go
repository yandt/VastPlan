package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProvisionIsIdempotentPrivateAndContained(t *testing.T) {
	root := filepath.Join(t.TempDir(), "provider")
	service, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Provision("repository.primary")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Provision("repository.primary")
	if err != nil || first.Handle != second.Handle || first.MountPath != second.MountPath {
		t.Fatalf("重复 provision 必须幂等: first=%+v second=%+v err=%v", first, second, err)
	}
	info, err := os.Stat(first.MountPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 || filepath.Dir(first.MountPath) != root {
		t.Fatalf("volume 必须私有且位于 provider root: path=%s mode=%v", first.MountPath, info.Mode())
	}
	if _, err := service.Provision("../escape"); err == nil {
		t.Fatal("目录逃逸 volume id 必须拒绝")
	}
}

func TestNewRejectsWorldReadableRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "provider")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root); err == nil {
		t.Fatal("非私有 provider root 必须拒绝")
	}
}
