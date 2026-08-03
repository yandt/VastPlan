package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

var pluginVersionDeclarationNames = map[string]struct{}{
	"PluginVersion": {},
	"pluginVersion": {},
	"version":       {},
}

// Backend 进程握手版本必须与签名 Manifest 使用同一身份。该门禁扫描包级
// 字符串常量，避免只提升 Manifest/Profile 后把旧版本二进制打入新 stable 制品。
func TestArch_BackendPluginVersionMatchesManifest(t *testing.T) {
	pluginsRoot := filepath.Join(repoRoot(t), "extensions", "plugins")
	entries, err := os.ReadDir(pluginsRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(pluginsRoot, entry.Name())
		manifestRaw, err := os.ReadFile(filepath.Join(directory, "vastplan.plugin.json"))
		if err != nil {
			continue
		}
		manifest, err := pluginv1.ParseManifest(manifestRaw)
		if err != nil {
			t.Errorf("解析 %s Manifest: %v", entry.Name(), err)
			continue
		}
		if manifest.Entry["backend"] == "" || manifest.Execution == nil || manifest.Execution.Backend == nil || manifest.Execution.Backend.Driver != "native" {
			continue
		}
		versions, err := packageLevelPluginVersions(directory)
		if err != nil {
			t.Errorf("扫描 %s Backend 版本: %v", manifest.ID, err)
			continue
		}
		if len(versions) == 0 {
			t.Errorf("Backend 插件 %s 未声明可校验的 PluginVersion/pluginVersion/version 字符串常量", manifest.ID)
			continue
		}
		for source, version := range versions {
			if version != manifest.Version {
				t.Errorf("Backend 插件版本身份漂移: %s Manifest=%s %s=%s", manifest.ID, manifest.Version, source, version)
			}
		}
	}
}

func packageLevelPluginVersions(root string) (map[string]string, error) {
	versions := map[string]string{}
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST && general.Tok != token.VAR {
				continue
			}
			for _, item := range general.Specs {
				value, ok := item.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range value.Names {
					if _, tracked := pluginVersionDeclarationNames[name.Name]; !tracked || index >= len(value.Values) {
						continue
					}
					literal, ok := value.Values[index].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					version, err := strconv.Unquote(literal.Value)
					if err != nil {
						return err
					}
					relative, err := filepath.Rel(root, path)
					if err != nil {
						return err
					}
					versions[filepath.ToSlash(relative)+":"+name.Name] = version
				}
			}
		}
		return nil
	})
	return versions, err
}
