package arch

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var directSQLPattern = regexp.MustCompile(`(?i)\b(select\s+.+\s+from|insert\s+into|update\s+[a-z0-9_]+\s+set|delete\s+from)\b`)

func TestDataModelLayeringBoundaries(t *testing.T) {
	root := filepath.Join(repoRoot(t), "extensions", "plugins")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".go" && ext != ".ts" && ext != ".tsx" && ext != ".py" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		normalized := filepath.ToSlash(path)
		if strings.Contains(normalized, "/frontend/src/") && importGeneratedRepository(raw) {
			t.Errorf("Frontend 不得导入 Repository 生成物: %s", normalized)
		}
		if isWorkflowSource(normalized) && directSQLPattern.Match(raw) {
			t.Errorf("Application Workflow 不得直接包含 SQL: %s", normalized)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func importGeneratedRepository(raw []byte) bool {
	text := strings.ToLower(string(raw))
	return strings.Contains(text, "generated/typescript") ||
		strings.Contains(text, "../generated/") ||
		strings.Contains(text, "./generated/")
}

func isWorkflowSource(path string) bool {
	return strings.Contains(path, "/workflow/") || strings.Contains(path, "/workflows/") ||
		strings.Contains(filepath.Base(path), "_workflow.") || strings.Contains(filepath.Base(path), "workflow_.")
}
