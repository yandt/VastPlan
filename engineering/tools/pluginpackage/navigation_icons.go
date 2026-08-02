package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func normalizeNavigationIcons(packageSource string) error {
	manifest := filepath.Join(packageSource, "vastplan.plugin.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		return err
	}
	if !bytes.Contains(raw, []byte(`"sources"`)) {
		return nil
	}
	script, err := findNavigationIconNormalizer()
	if err != nil {
		return err
	}
	command := exec.Command("node", script, "--plugin-root", packageSource, "--manifest", manifest)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("导航 SVG 归一化失败: %w\n%s", err, bytes.TrimSpace(output))
	}
	return nil
}

func findNavigationIconNormalizer() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(directory, "engineering", "tools", "normalize-navigation-icons.mjs")
		if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
			return candidate, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", fmt.Errorf("找不到 engineering/tools/normalize-navigation-icons.mjs")
		}
		directory = parent
	}
}
