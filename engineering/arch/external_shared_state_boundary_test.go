package arch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const localFileBoundaryMarker = "vastplan:local-file-boundary "

var allowedLocalFileBoundaries = map[string]struct{}{
	"bootstrap-root":     {},
	"derived-projection": {},
	"provider-private":   {},
	"recovery-root":      {},
}

// Plugins whose durable truth is external must not silently regain a local
// writable truth source. Runtime topology does not decide storage ownership.
func TestExternallyOwnedDurableTruthHasNoUnclassifiedLocalWrites(t *testing.T) {
	root := repoRoot(t)
	inventory := readStateOwnershipInventory(t, root)
	ownershipByID := make(map[string]stateOwnershipRecord, len(inventory.StateOwnership))
	for _, ownership := range inventory.StateOwnership {
		ownershipByID[ownership.ID] = ownership
	}
	pluginsRoot := filepath.Join(root, "extensions", "plugins")
	entries, err := os.ReadDir(pluginsRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginRoot := filepath.Join(pluginsRoot, entry.Name())
		ownership, found := ownershipByID[entry.Name()]
		if !found || !hasExternallyOwnedDurableTruth(ownership.DurableTruth) {
			continue
		}
		err := filepath.WalkDir(pluginRoot, func(path string, item os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if item.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if err := validateProductionLocalWrites(path, raw, ownership); err != nil {
				t.Errorf("%s: %v", filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator))), err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestExternalDurableTruthSelectionIgnoresRuntimeStateModel(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		durable []string
		want    bool
	}{
		{name: "leader owned shared state", model: "leader-owned", durable: []string{"shared-state"}, want: true},
		{name: "external topology without durable truth", model: "external-shared", durable: nil, want: false},
		{name: "record store", model: "leader-owned", durable: []string{"record-store"}, want: true},
		{name: "platform control sql", model: "none", durable: []string{"platform-control-sql"}, want: true},
		{name: "bootstrap file only", model: "external-shared", durable: []string{"bootstrap-file"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasExternallyOwnedDurableTruth(test.durable); got != test.want {
				t.Fatalf("model=%q durable=%v: got %t want %t", test.model, test.durable, got, test.want)
			}
		})
	}
}

func TestProductionLocalWriteBoundaryValidation(t *testing.T) {
	writeSource := []byte("package plugin\nimport \"os\"\nfunc persist() { _ = os.WriteFile(\"state.json\", nil, 0o600) }\n")
	aliasedWriteSource := []byte("package plugin\nimport filesystem \"os\"\nfunc persist() { _ = filesystem.WriteFile(\"state.json\", nil, 0o600) }\n")
	dotImportedWriteSource := []byte("package plugin\nimport . \"os\"\nfunc persist() { _ = WriteFile(\"state.json\", nil, 0o600) }\n")
	tests := []struct {
		name      string
		ownership stateOwnershipRecord
		source    []byte
		want      string
	}{
		{name: "standard os import write fails", ownership: stateOwnershipRecord{ID: "plugin", DurableTruth: []string{"shared-state"}}, source: writeSource, want: "未分类本机写入"},
		{name: "aliased os import write fails", ownership: stateOwnershipRecord{ID: "plugin", DurableTruth: []string{"shared-state"}}, source: aliasedWriteSource, want: "未分类本机写入"},
		{name: "dot imported os is rejected", ownership: stateOwnershipRecord{ID: "plugin", DurableTruth: []string{"shared-state"}}, source: dotImportedWriteSource, want: "禁止 dot-import os"},
		{name: "declared boundary missing from inventory fails", ownership: stateOwnershipRecord{ID: "plugin", DurableTruth: []string{"shared-state"}}, source: append([]byte("// vastplan:local-file-boundary provider-private\n"), writeSource...), want: "未进入状态归属清单"},
		{name: "bootstrap write requires bootstrap durable truth", ownership: stateOwnershipRecord{ID: "plugin", DurableTruth: []string{"shared-state"}, LocalBoundaries: []string{"bootstrap-root"}}, source: append([]byte("// vastplan:local-file-boundary bootstrap-root\n"), writeSource...), want: "未声明 bootstrap-file"},
		{name: "declared bootstrap boundary is accepted", ownership: stateOwnershipRecord{ID: "plugin", DurableTruth: []string{"shared-state", "bootstrap-file"}, LocalBoundaries: []string{"bootstrap-root"}}, source: append([]byte("// vastplan:local-file-boundary bootstrap-root\n"), writeSource...), want: ""},
		{name: "declared provider private boundary is accepted", ownership: stateOwnershipRecord{ID: "plugin", DurableTruth: []string{"shared-state"}, LocalBoundaries: []string{"provider-private"}}, source: append([]byte("// vastplan:local-file-boundary provider-private\n"), writeSource...), want: ""},
		{name: "declared derived projection boundary is accepted", ownership: stateOwnershipRecord{ID: "plugin", DurableTruth: []string{"shared-state"}, LocalBoundaries: []string{"derived-projection"}}, source: append([]byte("// vastplan:local-file-boundary derived-projection\n"), writeSource...), want: ""},
		{name: "declared recovery root boundary is accepted", ownership: stateOwnershipRecord{ID: "plugin", DurableTruth: []string{"shared-state"}, LocalBoundaries: []string{"recovery-root"}}, source: append([]byte("// vastplan:local-file-boundary recovery-root\n"), writeSource...), want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateProductionLocalWrites("plugin.go", test.source, test.ownership)
			if test.want == "" && err != nil {
				t.Fatal(err)
			}
			if test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)) {
				t.Fatalf("err=%v, want containing %q", err, test.want)
			}
		})
	}
}

func hasExternallyOwnedDurableTruth(durableTruth []string) bool {
	for _, durable := range durableTruth {
		switch durable {
		case "shared-state", "record-store", "platform-control-sql":
			return true
		}
	}
	return false
}

func validateProductionLocalWrites(path string, source []byte, ownership stateOwnershipRecord) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("解析 Go 源码: %w", err)
	}
	hasWrite, err := hasLocalWriteCall(file)
	if err != nil {
		return err
	}
	if !hasWrite {
		return nil
	}
	boundary := declaredLocalFileBoundary(file)
	if _, allowed := allowedLocalFileBoundaries[boundary]; !allowed {
		return fmt.Errorf("存在未分类本机写入")
	}
	if !containsString(ownership.LocalBoundaries, boundary) {
		return fmt.Errorf("本机写入未进入状态归属清单: plugin=%s boundary=%s", ownership.ID, boundary)
	}
	if boundary == "bootstrap-root" && !containsString(ownership.DurableTruth, "bootstrap-file") {
		return fmt.Errorf("bootstrap-root 本机写入未声明 bootstrap-file durableTruth: plugin=%s", ownership.ID)
	}
	return nil
}

func hasLocalWriteCall(file *ast.File) (bool, error) {
	osImportNames, dotImportedOS := osImportNames(file)
	if dotImportedOS {
		return false, fmt.Errorf("禁止 dot-import os，无法安全审计本机写入")
	}
	if len(osImportNames) == 0 {
		return false, nil
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		packageName, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		if _, imported := osImportNames[packageName.Name]; !imported {
			return true
		}
		switch selector.Sel.Name {
		case "WriteFile", "Create", "CreateTemp", "OpenFile", "Rename", "Remove", "RemoveAll", "MkdirAll":
			found = true
			return false
		default:
			return true
		}
	})
	return found, nil
}

func osImportNames(file *ast.File) (map[string]struct{}, bool) {
	names := map[string]struct{}{}
	dotImported := false
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != "os" {
			continue
		}
		if spec.Name == nil {
			names["os"] = struct{}{}
			continue
		}
		switch spec.Name.Name {
		case "_":
			continue
		case ".":
			dotImported = true
		default:
			names[spec.Name.Name] = struct{}{}
		}
	}
	return names, dotImported
}

func declaredLocalFileBoundary(file *ast.File) string {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			_, value, found := strings.Cut(comment.Text, localFileBoundaryMarker)
			if !found {
				continue
			}
			fields := strings.Fields(value)
			if len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return ""
}
