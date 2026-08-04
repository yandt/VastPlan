package platformcontrol

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
)

func TestOwnerFileSecretIsBoundedAndClearedAfterCallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("secret-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := ResolveSecretSource(platformcontrolv1.SecretRef{Kind: "owner-file", Path: path}, "")
	if err != nil {
		t.Fatal(err)
	}
	var borrowed []byte
	if err := source.WithSecret(context.Background(), func(value []byte) error {
		borrowed = value
		if string(value) != "secret-value" {
			t.Fatalf("秘密内容错误: %q", value)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, value := range borrowed {
		if value != 0 {
			t.Fatal("回调结束后秘密缓冲区必须清零")
		}
	}
}

func TestSecretResolverRejectsTraversalAndBroadDevelopmentFile(t *testing.T) {
	if _, err := ResolveSecretSource(platformcontrolv1.SecretRef{Kind: "systemd-credential", Name: "../secret"}, "/run/credentials/service"); err == nil {
		t.Fatal("systemd credential 名称不得越界")
	}
	path := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	source, err := ResolveSecretSource(platformcontrolv1.SecretRef{Kind: "owner-file", Path: path}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := source.WithSecret(context.Background(), func([]byte) error { return nil }); err == nil {
		t.Fatal("开发秘密文件必须 owner-only")
	}
}
