package plugindev

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var developmentPluginRoots = []string{
	filepath.Join("extensions", "plugins"),
	filepath.Join("examples", "plugins"),
}

func resolvePluginCandidate(repositoryRoot, selector string) (string, string, error) {
	if filepath.IsAbs(selector) || strings.Contains(selector, string(filepath.Separator)) || strings.HasPrefix(selector, ".") {
		candidate := selector
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(repositoryRoot, candidate)
		}
		return validatePluginCandidate(repositoryRoot, candidate)
	}
	var matches []string
	for _, relativeRoot := range developmentPluginRoots {
		candidate := filepath.Join(repositoryRoot, relativeRoot, selector)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			matches = append(matches, candidate)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
	}
	if len(matches) == 0 {
		return "", "", fmt.Errorf("插件 %s 不存在于 extensions/plugins 或 examples/plugins", selector)
	}
	if len(matches) > 1 {
		return "", "", fmt.Errorf("插件 %s 在产品与示例目录中重名", selector)
	}
	return validatePluginCandidate(repositoryRoot, matches[0])
}

func validatePluginCandidate(repositoryRoot, candidate string) (string, string, error) {
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", "", fmt.Errorf("解析插件目录: %w", err)
	}
	for _, relativeRoot := range developmentPluginRoots {
		root := filepath.Join(repositoryRoot, relativeRoot)
		realRoot, rootErr := filepath.EvalSymlinks(root)
		if rootErr != nil {
			if errors.Is(rootErr, os.ErrNotExist) {
				continue
			}
			return "", "", fmt.Errorf("解析插件根目录: %w", rootErr)
		}
		relative, relErr := filepath.Rel(realRoot, realCandidate)
		if relErr == nil && relative != "." && !filepath.IsAbs(relative) && !strings.Contains(relative, string(filepath.Separator)) {
			return realCandidate, filepath.ToSlash(relativeRoot), nil
		}
	}
	return "", "", errors.New("插件目录必须是 extensions/plugins 或 examples/plugins 的直接子目录")
}
