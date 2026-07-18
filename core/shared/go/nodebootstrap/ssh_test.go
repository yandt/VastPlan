package nodebootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOwnerFileRejectsLoosePrivateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(path, []byte("key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readOwnerFile(path, true); err == nil {
		t.Fatal("宽松权限 SSH 私钥必须被拒绝")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, err := readOwnerFile(path, true); err != nil || string(raw) != "key" {
		t.Fatalf("0600 私钥应可读取: raw=%q err=%v", raw, err)
	}
}

func TestLimitedBufferCapsUntrustedDiagnostics(t *testing.T) {
	buffer := &limitedBuffer{limit: 4}
	if n, err := buffer.Write([]byte("abcdefgh")); err != nil || n != 8 {
		t.Fatalf("Write 语义错误: n=%d err=%v", n, err)
	}
	if buffer.String() != "abcd" || strings.Contains(buffer.String(), "efgh") {
		t.Fatalf("远端诊断未截断: %q", buffer.String())
	}
}
