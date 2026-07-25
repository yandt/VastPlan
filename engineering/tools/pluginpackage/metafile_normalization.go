package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var (
	projectVirtualStorePrefix   = []byte(`.vastplan/cache/node/virtual-store/`)
	canonicalVirtualStorePrefix = []byte(`node_modules/.pnpm/`)
)

// normalizePackagedFrontendMetafiles removes the workspace-specific pnpm
// layout from build evidence after SBOM generation has consumed the original
// metafile. Stable package bytes must not depend on where pnpm stores packages.
func normalizePackagedFrontendMetafiles(root string) error {
	pattern := filepath.Join(root, "frontend", "dist", "vastplan.*-metafile.json")
	filenames, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("枚举前端构建 metafile: %w", err)
	}
	for _, filename := range filenames {
		info, err := os.Lstat(filename)
		if err != nil {
			return fmt.Errorf("检查前端构建 metafile %s: %w", filename, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("前端构建 metafile 必须是普通文件: %s", filename)
		}
		raw, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("读取前端构建 metafile %s: %w", filename, err)
		}
		if !json.Valid(raw) {
			return fmt.Errorf("前端构建 metafile 不是有效 JSON: %s", filename)
		}
		normalized := bytes.ReplaceAll(raw, projectVirtualStorePrefix, canonicalVirtualStorePrefix)
		if bytes.Equal(raw, normalized) {
			continue
		}
		if err := os.WriteFile(filename, normalized, info.Mode().Perm()); err != nil {
			return fmt.Errorf("写入规范化前端构建 metafile %s: %w", filename, err)
		}
	}
	return nil
}
