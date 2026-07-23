package signingkey

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func TestGenerateCreatesOwnerOnlyNonOverwritableKey(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "keys", "private.pem")
	publicKey, err := Generate(filename)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filename)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("私钥权限无效: info=%v err=%v", info, err)
	}
	privateKey, err := pluginservice.LoadEd25519PrivateKeyPEM(filename)
	if err != nil || !bytes.Equal(privateKey.Public().(ed25519.PublicKey), publicKey) {
		t.Fatalf("生成的公私钥不匹配: %v", err)
	}
	if _, err := Generate(filename); err == nil {
		t.Fatal("密钥生成不得覆盖已有私钥")
	}
}
