package arch

import "testing"

func TestArch_PluginsMustNotImportCore(t *testing.T) {
	assertNoImport(t, collectGoFiles(t), "extensions/plugins/", "core/",
		"插件只能依赖公开 Contracts、SDK、Library 或 capability protocol，不得链接内核实现（ADR-0150）")
	assertNoImport(t, collectGoFiles(t), "examples/plugins/", "core/",
		"示例插件只能依赖公开 Contracts、SDK、Library 或 capability protocol，不得链接内核实现（ADR-0150）")
}

func TestArch_SDKMustNotImportCore(t *testing.T) {
	assertNoImport(t, collectGoFiles(t), "extensions/sdk/", "core/",
		"SDK 只能依赖公开 Contracts、Library 与第三方库，不得反向依赖内核实现（ADR-0150）")
}

func TestArch_PublicLibrariesArePure(t *testing.T) {
	files := collectGoFiles(t)
	assertNoImport(t, files, "extensions/libraries/", "core/", "公共 Library 不得依赖内核实现")
	assertNoImport(t, files, "extensions/libraries/", "extensions/plugins/", "公共 Library 不得依赖具体插件")
	assertNoImport(t, files, "extensions/libraries/", "extensions/sdk/", "纯 Library 不得取得 HostCall 或插件运行时能力")
}
