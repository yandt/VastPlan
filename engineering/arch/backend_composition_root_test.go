package arch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type dependencyAssignment struct {
	file     string
	function string
	line     int
}

// A dependency has one composition owner. Multiple writes make runtime
// behavior depend on assembly call order and can silently replace one provider
// with another.
func TestArch_BackendDependenciesHaveSingleCompositionOwner(t *testing.T) {
	directory := filepath.Join(repoRoot(t), "core", "kernels", "backend")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}

	assignments := map[string][]dependencyAssignment{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		fileset := token.NewFileSet()
		file, err := parser.ParseFile(fileset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("解析 %s: %v", entry.Name(), err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				statement, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for _, target := range statement.Lhs {
					field, ok := dependencyField(target)
					if !ok {
						continue
					}
					assignments[field] = append(assignments[field], dependencyAssignment{
						file: entry.Name(), function: function.Name.Name, line: fileset.Position(target.Pos()).Line,
					})
				}
				return true
			})
		}
	}
	if len(assignments) == 0 {
		t.Fatal("未发现 Backend Dependencies 装配写入，门禁可能已失效")
	}

	fields := make([]string, 0, len(assignments))
	for field := range assignments {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		locations := assignments[field]
		if len(locations) <= 1 {
			continue
		}
		formatted := make([]string, 0, len(locations))
		for _, location := range locations {
			formatted = append(formatted, fmt.Sprintf("%s:%d (%s)", location.file, location.line, location.function))
		}
		t.Errorf("Dependencies.%s 存在多个组合根写点: %s", field, strings.Join(formatted, ", "))
	}
}

func dependencyField(target ast.Expr) (string, bool) {
	field, ok := target.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	dependencies, ok := field.X.(*ast.SelectorExpr)
	if !ok || dependencies.Sel.Name != "Dependencies" {
		return "", false
	}
	return field.Sel.Name, true
}
