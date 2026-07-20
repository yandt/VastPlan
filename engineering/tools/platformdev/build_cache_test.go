package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCachedBuildBuildsOnceAndRejectsIncompleteEntry(t *testing.T) {
	cacheRoot := t.TempDir()
	digest := digestStrings("fixture-v1")
	builds := 0
	build := func(candidate string) error {
		builds++
		output := filepath.Join(candidate, "out")
		if err := os.MkdirAll(output, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(output, "artifact"), []byte("immutable"), 0o700)
	}
	validate := func(candidate string) error {
		return requireCachedFiles(filepath.Join(candidate, "out"), "artifact")
	}

	first, err := ensureCachedBuild(cacheRoot, "fixture", digest, build, validate)
	if err != nil || first.Hit || builds != 1 {
		t.Fatalf("首次构建结果异常: build=%+v builds=%d err=%v", first, builds, err)
	}
	second, err := ensureCachedBuild(cacheRoot, "fixture", digest, build, validate)
	if err != nil || !second.Hit || builds != 1 || second.Path != first.Path {
		t.Fatalf("相同摘要必须命中缓存: build=%+v builds=%d err=%v", second, builds, err)
	}
	materialized := filepath.Join(t.TempDir(), "materialized")
	if err := materializeCachedDirectory(filepath.Join(second.Path, "out"), materialized); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(materialized, "artifact")); err != nil || string(content) != "immutable" {
		t.Fatalf("装配缓存产物失败: content=%q err=%v", content, err)
	}

	if err := os.Remove(filepath.Join(second.Path, "out", "artifact")); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := ensureCachedBuild(cacheRoot, "fixture", digest, build, validate)
	if err != nil || rebuilt.Hit || builds != 2 {
		t.Fatalf("不完整缓存必须原子重建: build=%+v builds=%d err=%v", rebuilt, builds, err)
	}
}

func TestDigestBuildInputsTracksSourcesButIgnoresGeneratedDirectories(t *testing.T) {
	root := t.TempDir()
	write := func(relative, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("module/main.go", "package module\n")
	write("module/dist/generated.js", "generated-v1")
	write("module/node_modules/dependency.js", "dependency-v1")
	initial, err := digestBuildInputs(root, []string{"module"}, []string{"toolchain-v1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	write("module/dist/generated.js", "generated-v2")
	write("module/node_modules/dependency.js", "dependency-v2")
	generatedChange, err := digestBuildInputs(root, []string{"module"}, []string{"toolchain-v1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if generatedChange != initial {
		t.Fatal("dist/node_modules 变化不得使源码摘要失效")
	}
	write("module/main.go", "package module\n// changed\n")
	sourceChange, err := digestBuildInputs(root, []string{"module"}, []string{"toolchain-v1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sourceChange == initial {
		t.Fatal("源码变化必须使构建摘要失效")
	}
	toolchainChange, err := digestBuildInputs(root, []string{"module"}, []string{"toolchain-v2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if toolchainChange == sourceChange {
		t.Fatal("工具链身份变化必须使构建摘要失效")
	}
}
