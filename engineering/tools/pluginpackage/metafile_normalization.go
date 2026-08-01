package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func hasPackagedFrontendMetafiles(root string) (bool, error) {
	filenames, err := filepath.Glob(filepath.Join(root, "frontend", "dist", "vastplan.*-metafile.json"))
	if err != nil {
		return false, fmt.Errorf("枚举前端构建 metafile: %w", err)
	}
	return len(filenames) > 0, nil
}

// removePackagedFrontendMetafiles drops esbuild's diagnostic metafiles after
// SBOM generation and Module Graph verification have consumed them. They
// include non-runtime source byte counts and physical dependency paths, so
// shipping them would make stable package identity depend on irrelevant build
// evidence. The signed Module Graph remains the runtime loading truth.
func removePackagedFrontendMetafiles(root string) error {
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
		if err := os.Remove(filename); err != nil {
			return fmt.Errorf("移除非运行期前端 metafile %s: %w", filename, err)
		}
	}
	return nil
}
