package nodeagent

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pythonlock"
)

type recordingPythonInstaller struct {
	packages int
}

func (*recordingPythonInstaller) Name() string { return "recording" }

func (i *recordingPythonInstaller) Materialize(_ context.Context, root string, summary pythonlock.Summary) error {
	i.packages = len(summary.Packages)
	return os.MkdirAll(pythonSitePackages(root), 0o755)
}

func TestPreparePythonEnvironmentUsesVerifiedOfflineLock(t *testing.T) {
	root := t.TempDir()
	wheel := []byte("wheel bytes")
	wheelDigest := sha256.Sum256(wheel)
	wheelName := "demo_dependency-1.2.3-py3-none-any.whl"
	wheelDirectory := filepath.Join(root, filepath.FromSlash(pythonlock.WheelPathPrefix))
	if err := os.MkdirAll(wheelDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wheelDirectory, wheelName), wheel, 0o600); err != nil {
		t.Fatal(err)
	}
	lock := []byte(fmt.Sprintf(`lock-version="1.0"
requires-python=">=3.11"
created-by="test"
packages=[{name="demo-dependency",version="1.2.3",wheels=[{path="python-wheels/%s",size=%d,hashes={sha256="%s"}}]}]
`, wheelName, len(wheel), hex.EncodeToString(wheelDigest[:])))
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(pythonlock.PackagePath)), lock, 0o600); err != nil {
		t.Fatal(err)
	}
	lockDigest := sha256.Sum256(lock)
	manifest, err := pluginv1.ParseManifest([]byte(fmt.Sprintf(`{"id":"com.example.python","name":"python","description":"test","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"python","requirements":{"python":">=3.11","demo-dependency":"1.2.3"}}},"activation":["onStartup"],"entry":{"backend":"backend/main.py"},"supplyChain":{"pythonLock":{"format":"pylock-toml","specVersion":"1.0","path":"supply-chain/pylock.toml","sha256":"%s"}},"contributes":{"backend":{"tools":[]}}}`, hex.EncodeToString(lockDigest[:]))))
	if err != nil {
		t.Fatal(err)
	}
	installer := &recordingPythonInstaller{}
	if err := preparePythonEnvironment(root, manifest, installer); err != nil {
		t.Fatal(err)
	}
	pythonPath, err := inspectPythonEnvironment(root, manifest)
	if err != nil || installer.packages != 1 || pythonPath != pythonSitePackages(root) {
		t.Fatalf("Python 依赖环境未物化: path=%s packages=%d err=%v", pythonPath, installer.packages, err)
	}
	if err := os.WriteFile(filepath.Join(wheelDirectory, wheelName), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := preparePythonEnvironment(root, manifest, installer); err == nil {
		t.Fatal("已解包 wheel 被篡改时不得物化")
	}
}

func TestPipPythonDependencyInstallerInstallsOnlyLocalLockedWheel(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 不可用")
	}
	if err := exec.Command(python, "-m", "pip", "--version").Run(); err != nil {
		t.Skip("pip 不可用")
	}
	root := t.TempDir()
	wheelName := "demo_dependency-1.2.3-py3-none-any.whl"
	wheelDirectory := filepath.Join(root, filepath.FromSlash(pythonlock.WheelPathPrefix))
	if err := os.MkdirAll(wheelDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	wheel := minimalWheel(t)
	if err := os.WriteFile(filepath.Join(wheelDirectory, wheelName), wheel, 0o600); err != nil {
		t.Fatal(err)
	}
	summary := pythonlock.Summary{RequiresPython: ">=3.8", Packages: []pythonlock.Package{{Name: "demo-dependency", Version: "1.2.3"}}}
	if err := (PipPythonDependencyInstaller{Interpreter: python}).Materialize(context.Background(), root, summary); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(pythonSitePackages(root), "demo_dependency", "__init__.py")); err != nil || string(raw) != "VALUE = 123\n" {
		t.Fatalf("离线 wheel 未物化到插件 overlay: raw=%q err=%v", raw, err)
	}
}

func TestPipPythonDependencyInstallerRejectsInterpreterOutsideLock(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 不可用")
	}
	if err := verifyPythonVersion(context.Background(), python, ">=99"); err == nil {
		t.Fatal("解释器不满足 requires-python 时必须拒绝")
	}
}

func minimalWheel(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	files := map[string]string{
		"demo_dependency/__init__.py":              "VALUE = 123\n",
		"demo_dependency-1.2.3.dist-info/METADATA": "Metadata-Version: 2.1\nName: demo-dependency\nVersion: 1.2.3\n",
		"demo_dependency-1.2.3.dist-info/WHEEL":    "Wheel-Version: 1.0\nGenerator: vastplan-test\nRoot-Is-Purelib: true\nTag: py3-none-any\n",
		"demo_dependency-1.2.3.dist-info/RECORD":   "",
	}
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
