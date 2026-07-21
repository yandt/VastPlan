package arch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var primitiveImport = regexp.MustCompile(`(?s)import\s+(?:type\s+)?\{([^}]*)\}\s+from\s+["']@vastplan/ui-primitives["']`)

// Functional frontend plugins declare pages through Workbench. Only the
// foundation frontend layer may own React/framework component trees.
func TestFunctionalFrontendPluginsUseWorkbenchBoundary(t *testing.T) {
	root := repoRoot(t)
	pluginRoot := filepath.Join(root, "extensions", "plugins")
	entries, err := os.ReadDir(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "cn.vastplan.foundation.frontend.") {
			continue
		}
		root := filepath.Join(pluginRoot, entry.Name())
		raw, err := os.ReadFile(filepath.Join(root, "vastplan.plugin.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Entry struct {
				Frontend string `json:"frontend"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest.Entry.Frontend == "" {
			continue
		}

		usesWorkbench := false
		err = filepath.WalkDir(filepath.Join(root, "frontend", "src"), func(path string, item os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if item.IsDir() || strings.Contains(item.Name(), ".test.") || strings.HasSuffix(item.Name(), ".d.ts") {
				return nil
			}
			if ext := filepath.Ext(path); ext != ".ts" && ext != ".tsx" && ext != ".js" && ext != ".jsx" {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			text := string(content)
			usesWorkbench = usesWorkbench || strings.Contains(text, `from "@vastplan/workbench-sdk"`) || strings.Contains(text, `from '@vastplan/workbench-sdk'`)
			for _, forbidden := range []string{`from "react"`, `from 'react'`, `from "react-dom`, `from 'react-dom`, `from "@arco-design/`, `from '@arco-design/`, `from "@mui/`, `from '@mui/`, "context.addPage("} {
				if strings.Contains(text, forbidden) {
					t.Errorf("%s: 功能插件越过 Workbench 边界，出现 %q", relative(root, path), forbidden)
				}
			}
			for _, match := range primitiveImport.FindAllStringSubmatch(text, -1) {
				for _, rawName := range strings.Split(match[1], ",") {
					name := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rawName), "type "))
					if name == "" {
						continue
					}
					name = strings.Fields(name)[0]
					if name != "" && !strings.HasPrefix(name, "Portal") {
						t.Errorf("%s: 功能插件只能从 ui-primitives 导入非视觉 Portal 契约，发现 %s", relative(root, path), name)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !usesWorkbench {
			t.Errorf("%s: 声明了前端入口但未使用 @vastplan/workbench-sdk", entry.Name())
		}
	}
}

func relative(root, path string) string {
	value, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return value
}
