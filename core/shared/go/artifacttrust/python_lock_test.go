package artifacttrust

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/pythonlock"
)

func TestInspectPackageBindsCompleteOfflinePythonLock(t *testing.T) {
	wheel := []byte("synthetic wheel bytes")
	wheelDigest := sha256.Sum256(wheel)
	wheelSHA := hex.EncodeToString(wheelDigest[:])
	lock := []byte(fmt.Sprintf(`lock-version = "1.0"
requires-python = ">=3.11"
created-by = "test"
extras = []
dependency-groups = []
default-groups = []

[[packages]]
name = "demo-dependency"
version = "1.2.3"

[[packages.wheels]]
path = "python-wheels/demo_dependency-1.2.3-py3-none-any.whl"
size = %d
hashes = {sha256 = "%s"}
`, len(wheel), wheelSHA))
	lockDigest := sha256.Sum256(lock)
	manifest := []byte(fmt.Sprintf(`{
  "id":"com.example.python-lock","name":"python-lock","description":"test","version":"1.0.0","publisher":"example",
  "engines":{"backend":"^1.0"},
  "execution":{"backend":{"driver":"python","requirements":{"python":">=3.11","demo-dependency":"1.2.3"}}},
  "activation":["onStartup"],"entry":{"backend":"backend/main.py"},
  "supplyChain":{"pythonLock":{"format":"pylock-toml","specVersion":"1.0","path":"supply-chain/pylock.toml","sha256":"%s"}},
  "contributes":{"backend":{"tools":[]}}
}`, hex.EncodeToString(lockDigest[:])))
	files := map[string][]byte{
		"vastplan.plugin.json": manifest,
		"backend/main.py":      []byte("print('ok')\n"),
		pythonlock.PackagePath: lock,
		pythonlock.WheelPathPrefix + "demo_dependency-1.2.3-py3-none-any.whl": wheel,
	}
	if _, _, err := InspectPackage(testArchiveFiles(t, files)); err != nil {
		t.Fatal(err)
	}
	files[pythonlock.WheelPathPrefix+"demo_dependency-1.2.3-py3-none-any.whl"] = []byte("tampered")
	if _, _, err := InspectPackage(testArchiveFiles(t, files)); err == nil {
		t.Fatal("wheel 字节与 pylock 摘要不一致时必须拒绝")
	}
}

func TestInspectPackageRequiresPythonLockForPythonDriver(t *testing.T) {
	manifest := []byte(`{"id":"com.example.python-unlocked","name":"python-unlocked","description":"test","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"python","requirements":{"python":">=3.11"}}},"activation":["onStartup"],"entry":{"backend":"backend/main.py"},"contributes":{"backend":{"tools":[]}}}`)
	if _, _, err := InspectPackage(testArchiveFiles(t, map[string][]byte{"vastplan.plugin.json": manifest, "backend/main.py": []byte("print('no lock')\n")})); err == nil {
		t.Fatal("Python 插件缺少 pylock.toml 必须 fail-closed")
	}
}
