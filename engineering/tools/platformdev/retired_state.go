package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type retiredDevelopmentStateFile struct {
	path        string
	replacement string
}

var retiredDevelopmentStateFiles = []retiredDevelopmentStateFile{
	{path: "state/api-exposure.json", replacement: "kernel.state.shared/api-exposure.control"},
	{path: "state/database-connections.json", replacement: "kernel.state.shared/platform.database.connections.v3"},
}

// quarantineRetiredDevelopmentStateFiles removes obsolete local truth sources
// from the active state directory without destroying the last local copy. The
// current plugins do not import these files: their durable truth is Shared State.
func quarantineRetiredDevelopmentStateFiles(stateRoot string, now time.Time) ([]string, error) {
	if !filepath.IsAbs(stateRoot) || filepath.Clean(stateRoot) != stateRoot {
		return nil, errors.New("退役状态清理要求规范绝对 state root")
	}
	timestamp := now.UTC().Format("20060102T150405.000000000Z")
	quarantineRoot := filepath.Join(stateRoot, "state", "quarantine", "retired-local-truth", timestamp)
	moved := make([]struct{ source, target string }, 0, len(retiredDevelopmentStateFiles))
	for _, retired := range retiredDevelopmentStateFiles {
		source := filepath.Join(stateRoot, filepath.FromSlash(retired.path))
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, rollbackRetiredStateMoves(moved, fmt.Errorf("检查退役状态文件 %s: %w", retired.path, err))
		}
		if !info.Mode().IsRegular() {
			return nil, rollbackRetiredStateMoves(moved, fmt.Errorf("退役状态路径不是普通文件: %s", retired.path))
		}
		target := filepath.Join(quarantineRoot, filepath.FromSlash(retired.path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return nil, rollbackRetiredStateMoves(moved, fmt.Errorf("创建退役状态隔离目录: %w", err))
		}
		if err := os.Rename(source, target); err != nil {
			return nil, rollbackRetiredStateMoves(moved, fmt.Errorf("隔离退役状态文件 %s: %w", retired.path, err))
		}
		moved = append(moved, struct{ source, target string }{source: source, target: target})
	}
	paths := make([]string, 0, len(moved))
	for _, item := range moved {
		relative, err := filepath.Rel(stateRoot, item.target)
		if err != nil {
			return nil, err
		}
		paths = append(paths, filepath.ToSlash(relative))
	}
	return paths, nil
}

func rollbackRetiredStateMoves(moved []struct{ source, target string }, cause error) error {
	for index := len(moved) - 1; index >= 0; index-- {
		if err := os.Rename(moved[index].target, moved[index].source); err != nil {
			return fmt.Errorf("%v；回滚 %s 失败: %w", cause, moved[index].source, err)
		}
	}
	return cause
}
