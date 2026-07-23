// Package arch 是**架构守护测试**（architecture fitness functions）。
//
// 我们对插件坚持 fail-closed，对自身架构约束却一度只靠自觉——并已被现实打脸：
// ADR-0017 §3 明写"协议常量禁止两处声明"，而 MVP 里 MagicCookie/ProtocolVersion
// 恰恰在 protocolbus 与 sdk 各写了一份，两轮后才发现。君子协定连规则作者都拦不住。
//
// 本包把文档里的架构约束变成**可执行断言**：违规即构建失败。
// 无 build tag —— 这些检查很快（只解析 import 与扫文件），应在每次 go test ./... 时生效。
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
)

const modulePath = "cdsoft.com.cn/VastPlan"

// ── 基础设施 ────────────────────────────────────────────

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd)) // engineering/arch/ 的上两级
}

// goFile 一个 Go 源文件及其解析出的导入。
type goFile struct {
	relPath   string // 相对仓库根，如 core/kernels/backend/main.go
	imports   []string
	generated bool // 由 codegen 产出（.pb.go）
}

// collectGoFiles 遍历仓库所有 Go 文件并解析其 import。
func collectGoFiles(t *testing.T) []goFile {
	t.Helper()
	root := repoRoot(t)
	var out []goFile

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "node_modules", ".obsidian":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		gf := goFile{
			relPath:   filepath.ToSlash(rel),
			generated: strings.HasSuffix(path, ".pb.go"),
		}
		for _, imp := range f.Imports {
			gf.imports = append(gf.imports, strings.Trim(imp.Path.Value, `"`))
		}
		out = append(out, gf)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历仓库失败: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("未扫描到任何 Go 文件——守护测试可能失效")
	}
	return out
}

// assertNoImport 断言 fromPrefix 下的文件不得 import toPrefix 下的包。
func assertNoImport(t *testing.T, files []goFile, fromPrefix, toPrefix, why string) {
	t.Helper()
	to := modulePath + "/" + toPrefix
	for _, f := range files {
		if !strings.HasPrefix(f.relPath, fromPrefix) {
			continue
		}
		for _, imp := range f.imports {
			if strings.HasPrefix(imp, to) {
				t.Errorf("依赖方向违规：%s 不得 import %s\n  文件: %s\n  导入: %s\n  原因: %s",
					fromPrefix, toPrefix, f.relPath, imp, why)
			}
		}
	}
}

// ── 依赖方向（工程规范 §6）────────────────────────────────

// 内核不认识任何具体插件——否则微内核就名存实亡。
func TestArch_KernelsMustNotImportPlugins(t *testing.T) {
	files := collectGoFiles(t)
	assertNoImport(t, files, "core/kernels/", "extensions/plugins/",
		"内核只提供骨架与扩展点，不得依赖任何具体插件（ADR-0001/0016）")
}

// 共享库不得反向依赖具体组件。
func TestArch_SharedMustNotImportComponents(t *testing.T) {
	files := collectGoFiles(t)
	assertNoImport(t, files, "core/shared/", "core/kernels/", "共享库不得反向依赖内核实现")
	assertNoImport(t, files, "core/shared/", "extensions/plugins/", "共享库不得依赖具体插件")
}

// SDK 是给插件开发者用的，不该依赖内核实现。
func TestArch_SDKMustNotImportKernels(t *testing.T) {
	files := collectGoFiles(t)
	assertNoImport(t, files, "extensions/sdk/", "core/kernels/",
		"SDK 面向插件开发者，只应依赖 proto 契约与 shared，不得依赖内核实现")
	assertNoImport(t, files, "extensions/sdk/", "extensions/plugins/", "SDK 不得依赖具体插件")
}

// Backend 普通子包不得横向抓取同级实现。组合只发生在根 main 与 commands；
// 其余少数有向依赖逐项登记，新增边必须先做架构决策。
func TestArch_BackendSiblingImportsAreExplicit(t *testing.T) {
	allowed := map[string]map[string]bool{
		"kernelops":         {"nodeagent": true},
		"nodeagent":         {"hostfactory": true},
		"profileactivation": {"platformcatalog": true}, // ADR-0116: trusted controller owns the exact candidate Store state machine.
	}
	backendImport := modulePath + "/core/kernels/backend/"
	for _, file := range collectGoFiles(t) {
		if !strings.HasPrefix(file.relPath, "core/kernels/backend/") || strings.HasSuffix(file.relPath, "_test.go") {
			continue
		}
		rel := strings.TrimPrefix(file.relPath, "core/kernels/backend/")
		parts := strings.Split(rel, "/")
		if len(parts) == 1 || parts[0] == "commands" {
			continue // 根 main 与命令包是显式组合根。
		}
		source := parts[0]
		for _, imported := range file.imports {
			if !strings.HasPrefix(imported, backendImport) {
				continue
			}
			target := strings.Split(strings.TrimPrefix(imported, backendImport), "/")[0]
			if target != source && !allowed[source][target] {
				t.Errorf("Backend 横向依赖未登记：%s (%s -> %s)\n  原因: 普通子包依赖 DTO/接口，不直接抓取同级实现（ADR-0040）",
					file.relPath, source, target)
			}
		}
	}
}

// 插件之间不得直接 import——只能经能力名寻址，否则绕过扩展点、架构失效。
func TestArch_PluginsMustNotImportEachOther(t *testing.T) {
	files := collectGoFiles(t)
	for _, f := range files {
		if !strings.HasPrefix(f.relPath, "extensions/plugins/") {
			continue
		}
		// 本插件自身的目录，如 extensions/plugins/cn.vastplan.hello-world
		rel := strings.TrimPrefix(f.relPath, "extensions/plugins/")
		parts := strings.SplitN(rel, "/", 2)
		if len(parts) < 2 {
			continue
		}
		own := modulePath + "/extensions/plugins/" + parts[0]

		for _, imp := range f.imports {
			if !strings.HasPrefix(imp, modulePath+"/extensions/plugins/") {
				continue
			}
			if !strings.HasPrefix(imp, own) {
				t.Errorf("插件间直接依赖违规：%s 不得 import 其他插件\n  导入: %s\n  原因: 插件间只能经 capability 名寻址（系统架构 §2.7）",
					f.relPath, imp)
			}
		}
	}
}

// clientcore 只服务 Runner 与 Mobile（ADR-0014），后端不得使用。
func TestArch_ClientCoreOnlyForRunnerAndMobile(t *testing.T) {
	files := collectGoFiles(t)
	cc := modulePath + "/core/shared/go/clientcore"
	for _, f := range files {
		for _, imp := range f.imports {
			if !strings.HasPrefix(imp, cc) {
				continue
			}
			allowed := strings.HasPrefix(f.relPath, "core/kernels/runner/") ||
				strings.HasPrefix(f.relPath, "core/kernels/mobile/") ||
				strings.HasPrefix(f.relPath, "core/shared/go/clientcore/")
			if !allowed {
				t.Errorf("clientcore 使用越界：%s 不得 import clientcore\n  原因: clientcore 只放 Runner 与 Mobile 共用的东西（ADR-0014）",
					f.relPath)
			}
		}
	}
}

// ── 单一真源（工程规范 §5）──────────────────────────────

// 协议常量只许在 core/shared/go/protocol 定义。
// 这正是 ADR-0017 §3 曾被违反的那条——本测试确保它不再重演。
//
// 注意：needle 由真源常量在运行时构造（而非在本文件硬编码字面量），
// 既避免守护测试自我误报，也使常量改值时守护自动跟随。
func TestArch_ProtocolConstantsSingleSource(t *testing.T) {
	root := repoRoot(t)
	magicLiteral := strconv.Quote(protocol.MagicCookie)
	const singleSource = "core/shared/go/protocol/"

	for _, f := range collectGoFiles(t) {
		if strings.HasPrefix(f.relPath, singleSource) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, f.relPath))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f.relPath, err)
		}
		if strings.Contains(string(b), magicLiteral) {
			t.Errorf("协议常量重复声明：%s 出现了 magic cookie 字面量\n  原因: 协议常量只许在 %s 定义（ADR-0017 §3）——两处声明会导致版本协商因两侧漂移而失效",
				f.relPath, singleSource)
		}
	}
}

// 内核稳定错误码只许在 core/shared/go/errorcode 定义；调用链必须引用常量。
// 否则字符串改名不会触发编译失败，旧插件和调用方会在运行时静默分叉。
func TestArch_KernelErrorCodesSingleSource(t *testing.T) {
	root := repoRoot(t)
	const singleSource = "core/shared/go/errorcode/"
	for _, f := range collectGoFiles(t) {
		if strings.HasPrefix(f.relPath, singleSource) || strings.HasSuffix(f.relPath, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, f.relPath))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f.relPath, err)
		}
		for _, code := range errorcode.KernelCodes() {
			if strings.Contains(string(b), strconv.Quote(code)) {
				t.Errorf("稳定错误码被散写：%s 出现 %q 字面量\n  原因: 内核错误码只许在 %s 定义并通过常量引用（ADR-0031）",
					f.relPath, code, singleSource)
			}
		}
	}
}

// 契约结构只由 proto 生成，不得手写。
func TestArch_ContractStructsAreGeneratedOnly(t *testing.T) {
	root := repoRoot(t)
	contractTypes := []string{"CallContext", "CallTarget", "CallResult", "CallEvent"}

	for _, f := range collectGoFiles(t) {
		if f.generated || strings.HasSuffix(f.relPath, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, f.relPath))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f.relPath, err)
		}
		src := string(b)
		for _, ct := range contractTypes {
			re := regexp.MustCompile(`(?m)^type\s+` + ct + `\s+struct`)
			if re.MatchString(src) {
				t.Errorf("契约结构被手写：%s 声明了 type %s struct\n  原因: 契约只由 contracts/proto/ 生成（ADR-0016 §6），手写副本必然与真源漂移",
					f.relPath, ct)
			}
		}
	}
}

func TestArch_StableDTOsHaveSingleStructSource(t *testing.T) {
	root := repoRoot(t)
	want := map[string]string{
		"StateIdentity":        "contracts/schemas/plugin/v1/schema.go",
		"MigrationRequest":     "contracts/schemas/plugin/v1/schema.go",
		"ResourceList":         "contracts/schemas/common/v1/resources.go",
		"ResourceRequirements": "contracts/schemas/common/v1/resources.go",
	}
	found := make(map[string][]string)
	for _, file := range collectGoFiles(t) {
		if file.generated || strings.HasSuffix(file.relPath, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, file.relPath))
		if err != nil {
			t.Fatal(err)
		}
		for name := range want {
			if regexp.MustCompile(`(?m)^type\s+` + name + `\s+struct\s*\{`).Match(raw) {
				found[name] = append(found[name], file.relPath)
			}
		}
	}
	for name, source := range want {
		if len(found[name]) != 1 || found[name][0] != source {
			t.Errorf("稳定 DTO %s 必须只在 %s 定义一次，实际: %v（ADR-0041）", name, source, found[name])
		}
	}
}

// ── 布局纪律（ADR-0016）─────────────────────────────────

// 根目录只允许五大产品/工程区域与明确的仓库工具目录。
// 新服务、新语言或新测试类型都应先归入已有区域，不得继续平铺。
func TestArch_TopLevelDirectoriesAreClosed(t *testing.T) {
	allowed := map[string]bool{
		"core": true, "extensions": true, "contracts": true, "engineering": true, "docs": true,
		".git": true, ".github": true, ".githooks": true, ".claude": true, ".codex": true,
		".gstack": true, ".obsidian": true, ".pnpm-store": true, ".vastplan": true,
		"bin": true, "node_modules": true,
	}
	entries, err := os.ReadDir(repoRoot(t))
	if err != nil {
		t.Fatalf("读取仓库根目录失败: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && !allowed[entry.Name()] {
			t.Errorf("根目录越界：%s/ 不在顶层白名单中；请归入 core/extensions/contracts/engineering/docs 之一，或先更新 ADR-0060", entry.Name())
		}
	}
}

// 服务组合是配置不是代码：不得出现 services/<role>/ 这类目录，
// 否则会诱导把 backend/workspace/rs 分叉成三份代码。
func TestArch_NoPerServiceDirectories(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "services")); err == nil {
		t.Errorf("布局违规：出现了 services/ 目录\n  原因: backend/workspace/rs 是同一 backend 内核二进制 + 不同期望态 service_role，" +
			"服务组合是配置不是代码（ADR-0016 §3）")
	}
}

func TestArch_ToolsMustNotContainProductionEntrypoints(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{"controlplane", "artifactserver"} {
		if _, err := os.Stat(filepath.Join(root, "engineering", "tools", name, "main.go")); err == nil {
			t.Errorf("engineering/tools/%s 不得承载生产入口；使用 Backend 子命令组合根（ADR-0040）", name)
		}
	}
}

// extensions/plugins/ 下只放产品插件，每个必须有清单；测试夹具插件应在 engineering/e2e/fixtures/。
func TestArch_EveryPluginHasManifest(t *testing.T) {
	root := repoRoot(t)
	pluginsDir := filepath.Join(root, "extensions", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatalf("读取 extensions/plugins/ 失败: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifest := filepath.Join(pluginsDir, e.Name(), "vastplan.plugin.json")
		raw, err := os.ReadFile(manifest)
		if err != nil {
			t.Errorf("插件缺少清单：extensions/plugins/%s 没有 vastplan.plugin.json\n  原因: extensions/plugins/ 只放产品插件且必须声明清单；"+
				"测试夹具插件应放 engineering/e2e/fixtures/plugins/（ADR-0018 §3）", e.Name())
			continue
		}
		parsed, err := pluginv1.ParseManifest(raw)
		if err != nil {
			t.Errorf("插件清单无效：extensions/plugins/%s/vastplan.plugin.json: %v", e.Name(), err)
			continue
		}
		if parsed.License != "Apache-2.0" || parsed.LicenseFile != "LICENSE" || parsed.NoticeFile != "NOTICE" {
			t.Errorf("第一方插件 %s 必须声明 license=Apache-2.0、licenseFile=LICENSE、noticeFile=NOTICE（ADR-0046）", e.Name())
		}
	}
}

func TestArch_ProjectLicenseDeclaration(t *testing.T) {
	root := repoRoot(t)
	license, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		t.Fatalf("仓库根目录必须包含 LICENSE: %v", err)
	}
	if !strings.Contains(string(license), "Apache License") ||
		!strings.Contains(string(license), "Version 2.0, January 2004") ||
		!strings.Contains(string(license), "Copyright [yyyy] [name of copyright owner]") {
		t.Fatal("LICENSE 必须保持可识别的 Apache-2.0 官方文本，不得写入项目自定义条款（ADR-0046）")
	}
	notice, err := os.ReadFile(filepath.Join(root, "NOTICE"))
	if err != nil {
		t.Fatalf("仓库根目录必须包含 NOTICE: %v", err)
	}
	if !strings.Contains(string(notice), "Copyright 2026 zhanghui") {
		t.Fatal("NOTICE 必须保留当前个人版权主体（ADR-0046）")
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("读取 README: %v", err)
	}
	if !strings.Contains(string(readme), "[Apache License 2.0](LICENSE)") ||
		!strings.Contains(string(readme), "[NOTICE](NOTICE)") {
		t.Fatal("README 必须提供 LICENSE 与 NOTICE 的可点击入口（ADR-0046）")
	}
}

// ── 文档纪律 ────────────────────────────────────────────

var mdLinkRe = regexp.MustCompile(`\]\(([^)]+\.md)(#[^)]*)?\)`)

// 文档不得有死链——此前一直靠手工 grep，现固化为测试。
func TestArch_DocsHaveNoDeadLinks(t *testing.T) {
	root := repoRoot(t)
	docsRoot := filepath.Join(root, "docs")

	err := filepath.WalkDir(docsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range mdLinkRe.FindAllStringSubmatch(string(b), -1) {
			link := m[1]
			if strings.HasPrefix(link, "http") || strings.HasPrefix(link, "/") {
				continue
			}
			target := filepath.Join(filepath.Dir(path), link)
			if _, err := os.Stat(target); err != nil {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("文档死链：%s → %s（目标不存在）", filepath.ToSlash(rel), link)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历文档失败: %v", err)
	}
}
