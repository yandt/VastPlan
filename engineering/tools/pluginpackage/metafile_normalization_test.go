package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

func TestStagePackageExcludesDiagnosticMetafilesWithoutChangingStableBytes(t *testing.T) {
	legacy := writeMetafilePackage(t, "node_modules/.pnpm/react@18.3.1/node_modules/react/index.js")
	projectCache := writeMetafilePackage(t, ".vastplan/cache/node/virtual-store/react@18.3.1/node_modules/react/index.js")

	legacyBytes := packageStagedMetafileFixture(t, legacy)
	projectCacheBytes := packageStagedMetafileFixture(t, projectCache)
	if !bytes.Equal(legacyBytes, projectCacheBytes) {
		t.Fatal("同一依赖的 pnpm 物理位置不得改变 stable 插件制品字节")
	}
}

func TestStagePackageDoesNotBypassMetafileRemovalForSourceOnlyPackage(t *testing.T) {
	source := writeMetafilePackage(t, "node_modules/.pnpm/react@18.3.1/node_modules/react/index.js")
	staged, cleanup := stagePackage(stagingOptions{Source: source})
	defer cleanup()
	if staged == source {
		t.Fatal("含 diagnostic metafile 的源码目录必须进入隔离 staging")
	}
	if _, err := os.Stat(filepath.Join(staged, "frontend", "dist", "vastplan.server-metafile.json")); !os.IsNotExist(err) {
		t.Fatalf("source-only 正式包不得保留 diagnostic metafile: %v", err)
	}
}

func packageStagedMetafileFixture(t *testing.T, source string) []byte {
	t.Helper()
	bundle := filepath.Join(source, "frontend", "dist", "index.js")
	staged, cleanup := stagePackage(stagingOptions{Source: source, FrontendBundle: bundle})
	defer cleanup()
	if _, err := os.Stat(filepath.Join(staged, "frontend", "dist", "vastplan.server-metafile.json")); !os.IsNotExist(err) {
		t.Fatalf("正式插件 staging 不得包含 esbuild diagnostic metafile: %v", err)
	}
	packageBytes, _, err := artifactrepository.PackageDirectory(staged)
	if err != nil {
		t.Fatal(err)
	}
	return packageBytes
}

func writeMetafilePackage(t *testing.T, dependencyPath string) string {
	t.Helper()
	root := t.TempDir()
	manifest := `{
  "id":"cn.vastplan.product.test.metafile","name":"metafile","description":"metafile","version":"1.0.0","publisher":"vastplan",
  "engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/dist/index.js"},
  "contributes":{"frontend":{"views":[{"id":"test.metafile","title":"Test"}]}}
}`
	files := map[string]string{
		"vastplan.plugin.json":                        manifest,
		"frontend/dist/index.js":                      "export default { register() {} };\n",
		"frontend/dist/vastplan.server-metafile.json": `{"inputs":{"` + dependencyPath + `":{"bytes":1}},"outputs":{"frontend/dist/server.js":{"bytes":1}}}`,
	}
	for relative, content := range files {
		filename := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
