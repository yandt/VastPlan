package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

func (r *runtime) restoreOrMigrateSeedRuntimeSnapshot() ([]artifactrepository.Ref, bool, error) {
	refs, restored, err := r.restoreSeedRuntimeSnapshot()
	if err != nil || restored {
		return refs, restored, err
	}
	source, found, err := r.findActiveHistoricalSeedRuntime()
	if err != nil || !found {
		return nil, false, err
	}
	log.Printf("迁移最近活动运行的 Seed Runtime 快照: %s", filepath.Base(source))
	if err := r.stageSeedRuntimeSnapshot(source, "historical-active-run"); err != nil {
		return nil, false, err
	}
	if err := r.commitSeedRuntimeSnapshot(); err != nil {
		return nil, false, err
	}
	return r.restoreSeedRuntimeSnapshot()
}

func (r *runtime) findActiveHistoricalSeedRuntime() (string, bool, error) {
	actualState, err := os.ReadFile(filepath.Join(r.persistentStateRoot(), "actual-state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var state struct {
		Units map[string]struct {
			Plugins []struct {
				Root string `json:"root"`
			} `json:"plugins"`
		} `json:"units"`
	}
	if err := json.Unmarshal(actualState, &state); err != nil {
		return "", false, fmt.Errorf("解析活动 ActualState: %w", err)
	}
	runsRoot := filepath.Join(r.options.stateRoot, "runs")
	runNames := map[string]struct{}{}
	for _, unit := range state.Units {
		for _, plugin := range unit.Plugins {
			name, ok := historicalRunNameForInstalledRoot(runsRoot, plugin.Root)
			if !ok {
				return "", false, fmt.Errorf("ActualState 插件安装根不属于开发运行目录: %q", plugin.Root)
			}
			runNames[name] = struct{}{}
		}
	}
	if len(runNames) == 0 {
		return "", false, nil
	}
	if len(runNames) != 1 {
		return "", false, fmt.Errorf("ActualState 同时引用 %d 个历史运行，无法安全选择 Seed Runtime 快照", len(runNames))
	}
	var name string
	for candidate := range runNames {
		name = candidate
	}
	run := filepath.Join(runsRoot, name)
	if err := validateHistoricalSeedRuntimeSource(run); err != nil {
		return "", false, fmt.Errorf("活动历史运行无法迁移为 Seed Runtime 快照: %w", err)
	}
	return run, true, nil
}

func historicalRunNameForInstalledRoot(runsRoot, installedRoot string) (string, bool) {
	if strings.TrimSpace(installedRoot) == "" || !filepath.IsAbs(installedRoot) {
		return "", false
	}
	relative, err := filepath.Rel(runsRoot, filepath.Clean(installedRoot))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) < 4 || parts[0] == "" || parts[1] != "installed" {
		return "", false
	}
	return parts[0], true
}

func validateHistoricalSeedRuntimeSource(root string) error {
	for _, path := range []string{"dynamic/backend-kernel", "dynamic/vastplan-go-dynamic-host", "portal-assets/index.html", "repository", "seed-inventory.json", "access-profile-catalog.json", "backend-platform-catalog.json"} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			return err
		}
	}
	return nil
}
