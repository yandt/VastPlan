package plugindev

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func SourceDigest(repositoryRoot string, spec Spec) (string, error) {
	roots := []string{spec.Relative}
	switch spec.Driver {
	case DriverNativeGo, DriverDynamicGo:
		roots = append(roots, "go.mod", "go.sum", "contracts", "core/shared/go", "extensions/sdk/go")
		if spec.Driver == DriverDynamicGo {
			roots = append(roots, "core/runtimehosts/go-dynamic", "engineering/tools/dynamicgofingerprint")
		}
	case DriverNodeWorker:
		roots = append(roots, "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "extensions/sdk/node/backend-plugin", "contracts/proto", "engineering/tools/build-node-backend-plugins.mjs")
	case DriverPython, DriverPythonInterpreter:
		roots = append(roots, "extensions/sdk/python", "contracts/proto")
	}
	if spec.FrontendEntry != "" {
		roots = append(roots, "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "extensions/sdk/ts", "core/kernels/frontend", "engineering/tools/build-frontend-plugins.mjs", "engineering/tools/frontend-module-graph.mjs")
	}
	roots = compactRoots(roots)
	sort.Strings(roots)
	hash := sha256.New()
	for _, relativeRoot := range roots {
		absolute := filepath.Join(repositoryRoot, filepath.FromSlash(relativeRoot))
		if err := filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(repositoryRoot, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			if entry.IsDir() {
				switch entry.Name() {
				case ".git", ".vastplan", "node_modules", "dist", "__pycache__", "graphify-out":
					if path != absolute {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("开发输入不允许符号链接: %s", relative)
			}
			if !entry.Type().IsRegular() || ignoredDevelopmentFile(relative) {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, _ = io.WriteString(hash, relative)
			_, _ = hash.Write([]byte{0})
			_, _ = hash.Write(raw)
			_, _ = hash.Write([]byte{0})
			return nil
		}); err != nil {
			return "", fmt.Errorf("计算开发输入 %s: %w", relativeRoot, err)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func compactRoots(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func ignoredDevelopmentFile(relative string) bool {
	base := filepath.Base(relative)
	return strings.HasSuffix(base, ".pyc") || strings.HasSuffix(base, ".pyo") || base == ".DS_Store"
}
