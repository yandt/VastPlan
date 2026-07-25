package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginBuildInputsFollowActualPublicDependencyClosure(t *testing.T) {
	root := repositoryRoot(t)
	runtime := &runtime{options: options{root: root, stateRoot: t.TempDir()}}
	goCache := filepath.Join(runtime.options.stateRoot, "go-cache")
	inputs, err := runtime.listGoBinaryInputs(context.Background(), goCache,
		"./extensions/plugins/cn.vastplan.demo-audit/backend",
		[]string{"extensions/plugins/cn.vastplan.demo-audit/vastplan.plugin.json", "go.mod", "go.sum"})
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
