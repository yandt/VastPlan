package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pythonlock"
)

func TestBindPythonLockVerifiesWheelAndUpdatesManifest(t *testing.T) {
	root := t.TempDir()
	wheelDirectory := filepath.Join(root, filepath.FromSlash(pythonlock.WheelPathPrefix))
	if err := os.MkdirAll(wheelDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	wheelName := "demo_dependency-1.2.3-py3-none-any.whl"
	wheel := []byte("wheel")
	digest := sha256.Sum256(wheel)
	if err := os.WriteFile(filepath.Join(wheelDirectory, wheelName), wheel, 0o600); err != nil {
		t.Fatal(err)
	}
	lock := fmt.Sprintf(`lock-version="1.0"
requires-python=">=3.11"
created-by="test"
packages=[{name="demo-dependency",version="1.2.3",wheels=[{path="python-wheels/%s",size=%d,hashes={sha256="%s"}}]}]
`, wheelName, len(wheel), hex.EncodeToString(digest[:]))
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(pythonlock.PackagePath)), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest([]byte(`{"id":"com.example.python","name":"python","description":"test","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"python","requirements":{"python":">=3.11","demo-dependency":"1.2.3"}}},"activation":["onStartup"],"entry":{"backend":"backend/main.py"},"contributes":{"backend":{"tools":[]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	changed, err := bindPythonLock(&manifest, root)
	if err != nil || !changed || manifest.SupplyChain == nil || manifest.SupplyChain.PythonLock == nil {
		t.Fatalf("Python 锁未绑定: changed=%t manifest=%#v err=%v", changed, manifest.SupplyChain, err)
	}
	if err := os.WriteFile(filepath.Join(wheelDirectory, wheelName), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest.SupplyChain.PythonLock = nil
	if _, err := bindPythonLock(&manifest, root); err == nil {
		t.Fatal("wheel 被篡改后必须拒绝")
	}
}
