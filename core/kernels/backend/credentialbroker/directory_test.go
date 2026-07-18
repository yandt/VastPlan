package credentialbroker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
)

func TestDirectoryScopesAndWipesMaterial(t *testing.T) {
	root := t.TempDir()
	tenant := filepath.Join(root, "tenant-a")
	if err := os.Mkdir(tenant, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(tenant, "ssh.identity")
	if err := os.WriteFile(path, []byte("secret-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	broker, err := NewDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	var observed []byte
	err = broker.WithCredential(context.Background(), kernelspi.Scope{TenantID: "tenant-a", PluginID: "plugin", Namespace: "node-bootstrap"}, kernelspi.CredentialRef{Name: "ssh.identity", Scope: "tenant"}, func(value kernelspi.CredentialMaterial) error {
		observed = value.Bytes()
		if string(observed) != "secret-value" {
			t.Fatalf("material 错误: %q", observed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range observed {
		if value != 0 {
			t.Fatal("回调结束后 material 必须擦除")
		}
	}
}

func TestDirectoryRejectsLooseMaterial(t *testing.T) {
	root := t.TempDir()
	tenant := filepath.Join(root, "tenant-a")
	if err := os.Mkdir(tenant, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tenant, "ssh.identity"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	broker, _ := NewDirectory(root)
	if err := broker.WithCredential(context.Background(), kernelspi.Scope{TenantID: "tenant-a", PluginID: "plugin", Namespace: "node-bootstrap"}, kernelspi.CredentialRef{Name: "ssh.identity", Scope: "tenant"}, func(kernelspi.CredentialMaterial) error { return nil }); err == nil {
		t.Fatal("宽松权限 material 必须被拒绝")
	}
}
