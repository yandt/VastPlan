package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSplitListNormalizesAndDeduplicates(t *testing.T) {
	actual := splitList(" vastplan,example,vastplan, ")
	if !reflect.DeepEqual(actual, []string{"vastplan", "example"}) {
		t.Fatalf("列表规范化无效: %#v", actual)
	}
}

func TestConfinedRunDirRejectsEscapes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	inside := filepath.Join(root, "runs", "run-1")
	if actual, err := confinedRunDir(root, inside); err != nil || actual != inside {
		t.Fatalf("合法运行目录被拒绝: %s %v", actual, err)
	}
	if _, err := confinedRunDir(root, filepath.Join(root, "runs-escape", "run-1")); err == nil {
		t.Fatal("越界运行目录必须拒绝")
	}
}

func TestReadSecretRequiresPrivateRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if value, err := readSecret(path); err != nil || value != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("私有凭证读取失败: %q %v", value, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecret(path); err == nil {
		t.Fatal("宽权限凭证必须拒绝")
	}
}
