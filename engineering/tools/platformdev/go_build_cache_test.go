package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginBuildInputsFollowActualPublicDependencyClosure(t *testing.T) {
	root := repositoryRoot(t)
	runtime := &runtime{options: options{root: root, stateRoot: t.TempDir()}}
	goCache := filepath.Join(runtime.options.stateRoot, "go-cache")
	inputs, err := runtime.listGoBinaryInputs(context.Background(), goCache,
		"./examples/plugins/cn.vastplan.example.backend.audit/backend",
		[]string{"examples/plugins/cn.vastplan.example.backend.audit/vastplan.plugin.json", "go.mod", "go.sum"})
	if err != nil {
		t.Fatal(err)
	}
	foundContract, foundSDK := false, false
	for _, input := range inputs {
		if strings.HasPrefix(input, "core/") {
			t.Fatalf("普通插件构建闭包不得包含内核实现: %s", input)
		}
		foundContract = foundContract || strings.HasPrefix(input, "contracts/")
		foundSDK = foundSDK || strings.HasPrefix(input, "extensions/sdk/go/")
	}
	if !foundContract || !foundSDK {
		t.Fatalf("插件构建闭包必须包含实际使用的 Contracts 和 SDK: contract=%t sdk=%t", foundContract, foundSDK)
	}
}

func TestPairPreparedBackendKernelReplacesStaleDynamicCopy(t *testing.T) {
	runDir := t.TempDir()
	runtime := &runtime{runDir: runDir}
	source := filepath.Join(runDir, "bin", "backend-kernel")
	target := filepath.Join(runDir, "dynamic", "backend-kernel")
	for _, path := range []string{source, target} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(source, []byte("current dependency-aware kernel"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("stale dynamic cache kernel"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := runtime.pairPreparedBackendKernel(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "current dependency-aware kernel" {
		t.Fatalf("实际 Kernel 运行入口没有跟随本次 Backend 构建: %q err=%v", raw, err)
	}
}
