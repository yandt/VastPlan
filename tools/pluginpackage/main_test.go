package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTreeSkipsPythonBytecode(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "main.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(source, "__pycache__")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "main.pyc"), []byte("bytecode"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(source, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "main.py")); err != nil {
		t.Fatal("Python 源文件必须进入制品")
	}
	if _, err := os.Stat(filepath.Join(target, "__pycache__")); !os.IsNotExist(err) {
		t.Fatalf("Python 字节码缓存不得进入制品: %v", err)
	}
}
