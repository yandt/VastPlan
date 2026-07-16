package nodeagent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
)

func TestLocalInstaller_ContentAddressedAndIdempotent(t *testing.T) {
	packageBytes, artifact := testPackage(t, 0o755)
	installer := LocalInstaller{Root: filepath.Join(t.TempDir(), "installed")}
	first, err := installer.Install(artifact, packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	second, err := installer.Install(artifact, packageBytes)
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
	if _, err := installer.Install(artifact, corrupt); err == nil {
		t.Fatal("损坏字节必须被 SHA-256 拒绝")
	}

	packageBytes, artifact = testPackage(t, 0o644)
	if _, err := installer.Install(artifact, packageBytes); err == nil {
		t.Fatal("不可执行 backend 入口必须被拒绝")
	}
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
