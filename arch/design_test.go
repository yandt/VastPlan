// 代码设计原则的守护断言（ADR-0020）。
//
// 内聚与复用不能只写在文档里——ADR-0019 的教训是君子协定连规则作者都拦不住。
// 这里守两条可机械判定的：包必须能说清职责（内聚）、不得有垃圾抽屉包（复用）。
package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// junkDrawerNames 垃圾抽屉包名：没有职责的包必然沦为万物堆场，内聚为零。
//
// 证据（ADR-0020）：testa 有 shared/（3,248 LOC）却只被 import 86 处（利用率 <2%），
// 同时背着 17,000-20,000 LOC 重复代码——目录建了、没人用、重复照旧疯长。
// 复用靠**有职责的包**（registry/protocol/contract…），不靠垃圾抽屉。
var junkDrawerNames = map[string]bool{
	"util": true, "utils": true,
	"common": true, "commons": true,
	"helper": true, "helpers": true,
	"misc": true, "base": true, "core": true,
	"shared": true, // shared/ 作为顶层分组目录可以，但不得有名为 shared 的 Go 包
}

// sourceTrees 我们自己写的代码（生成物与夹具不在此列）。
var sourceTrees = []string{"kernels", "shared", "sdk", "plugins", "arch", "e2e"}

// 不得出现垃圾抽屉包——按能力命名，一包一职责（ADR-0020 §3）。
func TestDesign_NoJunkDrawerPackages(t *testing.T) {
	root := repoRoot(t)

	for _, tree := range sourceTrees {
		treePath := filepath.Join(root, tree)
		if _, err := os.Stat(treePath); err != nil {
			continue // 该内核尚未落地
		}
		err := filepath.WalkDir(treePath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			name := d.Name()
			// shared/go 是分组目录（go/ts 语言分层），不是 Go 包；只查含 .go 文件的目录
			if !dirHasGoFiles(path) {
				return nil
			}
			if junkDrawerNames[strings.ToLower(name)] {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("垃圾抽屉包：%s\n  原因: 包名 %q 无职责，必然沦为万物堆场、内聚为零（ADR-0020 §3）\n"+
					"  改法: 按**能力**命名并保证一包一职责，如 registry / protocol / contract",
					filepath.ToSlash(rel), name)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败: %v", tree, err)
		}
	}
}

// 每个包必须有 doc comment 写明单一职责——这是**内聚的试金石**：
// 一句话说不清干什么（或出现"以及/还有/各种"），说明它已经不内聚，该拆（ADR-0020 §1）。
func TestDesign_EveryPackageHasDocComment(t *testing.T) {
	root := repoRoot(t)

	for _, tree := range sourceTrees {
		treePath := filepath.Join(root, tree)
		if _, err := os.Stat(treePath); err != nil {
			continue
		}
		err := filepath.WalkDir(treePath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() || !dirHasGoFiles(path) {
				return nil
			}
			if onlyGeneratedFiles(path) {
				return nil // 生成物不归我们管
			}
			if packageHasDoc(t, path) {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			t.Errorf("包缺少 doc comment：%s\n  原因: 包必须一句话说清单一职责——说不清即说明不内聚（ADR-0020 §1）\n"+
				"  改法: 在某个 .go 文件的 package 子句正上方（不留空行）写注释",
				filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			t.Fatalf("遍历 %s 失败: %v", tree, err)
		}
	}
}

// ── 辅助 ────────────────────────────────────────────────

func dirHasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

func onlyGeneratedFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	hasGo := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		hasGo = true
		if !strings.HasSuffix(e.Name(), ".pb.go") {
			return false
		}
	}
	return hasGo
}

// packageHasDoc 该目录下是否至少有一个文件为 package 子句写了 doc comment。
// 用 ast.File.Doc 判定——它正是"紧贴 package 子句上方的注释块"，即 Go 的包注释规则。
func packageHasDoc(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", dir, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".pb.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", name, err)
		}
		if hasNonEmptyDoc(f) {
			return true
		}
	}
	return false
}

func hasNonEmptyDoc(f *ast.File) bool {
	if f.Doc == nil {
		return false
	}
	return strings.TrimSpace(f.Doc.Text()) != ""
}
