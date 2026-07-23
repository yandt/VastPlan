package nodeagent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func TestLocalInstaller_ContentAddressedAndIdempotent(t *testing.T) {
	packageBytes, artifact := testPackage(t, 0o755)
	installer := LocalInstaller{Root: filepath.Join(t.TempDir(), "installed")}
	first, err := installer.Install(verifiedForTest(artifact, packageBytes))
	if err != nil {
		t.Fatal(err)
	}
	second, err := installer.Install(verifiedForTest(artifact, packageBytes))
	if err != nil {
		t.Fatal(err)
	}
	if first.Root != second.Root || filepath.Base(first.Root) != artifact.SHA256 {
		t.Fatalf("安装未按内容寻址: first=%+v second=%+v", first, second)
	}
	if info, err := os.Stat(first.EntryPath); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("backend 入口没有保留可执行位: info=%v err=%v", info, err)
	}
	if first.State == nil || first.State.Format != "com.example.installer.state" ||
		first.State.FormatVersion != 2 || len(first.State.MigrationFrom) != 1 {
		t.Fatalf("安装结果没有保留验签清单的状态契约: %+v", first.State)
	}
}

func TestLocalInstaller_RejectsCorruptBytesAndNonExecutableEntry(t *testing.T) {
	packageBytes, artifact := testPackage(t, 0o755)
	installer := LocalInstaller{Root: t.TempDir()}
	corrupt := append([]byte(nil), packageBytes...)
	corrupt[len(corrupt)-1] ^= 0xff
	if _, err := installer.Install(verifiedForTest(artifact, corrupt)); err == nil {
		t.Fatal("损坏字节必须被 SHA-256 拒绝")
	}

	packageBytes, artifact = testPackage(t, 0o644)
	if _, err := installer.Install(verifiedForTest(artifact, packageBytes)); err == nil {
		t.Fatal("不可执行 backend 入口必须被拒绝")
	}
}

func TestLocalInstaller_PythonEntryNeedNotBeExecutable(t *testing.T) {
	dir := t.TempDir()
	lock := []byte("lock-version=\"1.0\"\nrequires-python=\">=3.8\"\ncreated-by=\"test\"\npackages=[]\n")
	lockDigest := sha256.Sum256(lock)
	manifest := []byte(fmt.Sprintf(`{
  "id":"com.example.python","name":"python","description":"python","version":"1.0.0","publisher":"example",
  "engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"python","requirements":{"python":">=3.8"}}},
  "activation":["onStartup"],"entry":{"backend":"backend/main.py"},
  "supplyChain":{"pythonLock":{"format":"pylock-toml","specVersion":"1.0","path":"supply-chain/pylock.toml","sha256":"%s"}},
  "contributes":{"backend":{"tools":[{"id":"example.python","service_role":"backend"}]}}
}`, hex.EncodeToString(lockDigest[:])))
	if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backend", "main.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "supply-chain"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "supply-chain", "pylock.toml"), lock, 0o644); err != nil {
		t.Fatal(err)
	}
	packageBytes, _, err := pluginservice.PackageDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe("stable", packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := (LocalInstaller{Root: t.TempDir()}).Install(verifiedForTest(artifact, packageBytes))
	if err != nil {
		t.Fatal(err)
	}
	if installed.Publisher != "example" || installed.Execution.Driver != "python" || installed.PythonPath == "" {
		t.Fatalf("安装器没有冻结发布者和执行契约: %+v", installed)
	}
}

func TestLocalInstallerFreezesDynamicGoEntryFromSignedManifest(t *testing.T) {
	makePackage := func(t *testing.T, includeModule, includeFingerprint bool) ([]byte, pluginv1.Artifact) {
		t.Helper()
		dir := t.TempDir()
		fingerprint := ""
		if includeFingerprint {
			fingerprint = `,"fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`
		}
		manifest := []byte(fmt.Sprintf(`{
  "id":"cn.vastplan.foundation.test.dynamic","name":"dynamic","description":"dynamic","version":"1.0.0","publisher":"vastplan",
  "engines":{"backend":"^1.0"},"execution":{"backend":{"driver":"native","minimumIsolation":"trusted-process",
    "dynamicGo":{"entry":"backend/plugin.so","abi":"vastplan.dynamic-go.v1","required":true%s}}},
  "activation":["onStartup"],"entry":{"backend":"backend/plugin"},
  "contributes":{"backend":{"tools":[{"id":"foundation.test.dynamic.tool","service_role":"backend"}]}}
}`, fingerprint))
		if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), manifest, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "backend"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "backend", "plugin"), []byte("process"), 0o755); err != nil {
			t.Fatal(err)
		}
		if includeModule {
			if err := os.WriteFile(filepath.Join(dir, "backend", "plugin.so"), []byte("signed-so"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		packageBytes, _, err := pluginservice.PackageDirectory(dir)
		if err != nil {
			t.Fatal(err)
		}
		artifact, err := pluginservice.Describe("stable", packageBytes)
		if err != nil {
			t.Fatal(err)
		}
		return packageBytes, artifact
	}

	packageBytes, artifact := makePackage(t, true, true)
	installed, err := (LocalInstaller{Root: t.TempDir()}).Install(verifiedForTest(artifact, packageBytes))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(installed.DynamicGoPath) != "plugin.so" || installed.Execution.DynamicGo == nil ||
		!installed.Execution.DynamicGo.Required {
		t.Fatalf("安装结果没有冻结 dynamic-go 入口: %+v", installed)
	}

	packageBytes, artifact = makePackage(t, false, true)
	if _, err := (LocalInstaller{Root: t.TempDir()}).Install(verifiedForTest(artifact, packageBytes)); err == nil ||
		!strings.Contains(err.Error(), "dynamic-go 入口不存在") {
		t.Fatalf("清单声明但制品缺少 .so 时必须拒绝: %v", err)
	}

	packageBytes, artifact = makePackage(t, true, false)
	if _, err := (LocalInstaller{Root: t.TempDir()}).Install(verifiedForTest(artifact, packageBytes)); err == nil ||
		!strings.Contains(err.Error(), "缺少构建时注入") {
		t.Fatalf("未冻结共同构建指纹的发布包必须拒绝: %v", err)
	}
}

func TestLocalInstaller_EnforcesExpandedPackageQuotas(t *testing.T) {
	packageBytes, artifact := testPackage(t, 0o755)
	for name, installer := range map[string]LocalInstaller{
		"files":    {Root: t.TempDir(), MaxFiles: 1},
		"fileSize": {Root: t.TempDir(), MaxFileBytes: 1},
		"expanded": {Root: t.TempDir(), MaxExpandedBytes: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := installer.Install(verifiedForTest(artifact, packageBytes)); err == nil {
				t.Fatal("超过解包资源配额的插件包必须拒绝")
			}
		})
	}
}

func verifiedForTest(artifact pluginv1.Artifact, packageBytes []byte) VerifiedArtifact {
	return VerifiedArtifact{artifact: artifact, packageBytes: packageBytes, verified: true}
}

func TestLocalInstaller_GarbageCollectOnlyContentDirectories(t *testing.T) {
	root := t.TempDir()
	keep := strings.Repeat("a", 64)
	remove := strings.Repeat("b", 64)
	for _, name := range []string{keep, remove, ".install-leftover", "operator-data"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installer := LocalInstaller{Root: root}
	if err := installer.GarbageCollect([]string{keep}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{keep, ".install-leftover", "operator-data"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("不应清理目录 %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, remove)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("未引用内容目录应删除，实际 err=%v", err)
	}
}

func testPackage(t *testing.T, mode os.FileMode) ([]byte, pluginv1.Artifact) {
	t.Helper()
	dir := t.TempDir()
	manifest := []byte(`{
  "id":"com.example.installer","name":"installer test","description":"installer test",
  "version":"1.0.0","publisher":"example","engines":{"backend":"^0.1"},
  "state":{"backend":{"format":"com.example.installer.state","formatVersion":2,
    "migration":{"protocol":"lifecycle.v1","from":[{"format":"com.example.installer.state","formatVersion":1}]}}},
  "activation":["onStartup"],"entry":{"backend":"backend/plugin"},
	  "contributes":{"backend":{"tools":[{"id":"example.tool","service_role":"backend"}]}}
}`)
	if err := os.WriteFile(filepath.Join(dir, "vastplan.plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "backend", "plugin"), []byte("binary"), mode); err != nil {
		t.Fatal(err)
	}
	packageBytes, parsed, err := pluginservice.PackageDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe("stable", packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.PluginID != parsed.ID {
		t.Fatal("制品描述与已解析清单不一致")
	}
	return packageBytes, artifact
}
