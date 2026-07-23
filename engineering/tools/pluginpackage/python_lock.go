package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pythonlock"
)

func bindPythonLock(manifest *pluginv1.Manifest, source string) (bool, error) {
	if manifest == nil {
		return false, errors.New("Python 锁缺少插件清单")
	}
	execution := pluginv1.BackendExecutionContract(*manifest)
	pythonDriver := execution.Driver == "python" || execution.Driver == "python-subinterpreter"
	var declared *pluginv1.SupplyChainDocument
	if manifest.SupplyChain != nil {
		declared = manifest.SupplyChain.PythonLock
	}
	if !pythonDriver {
		if declared != nil {
			return false, errors.New("非 Python 插件不得声明 pythonLock")
		}
		return false, nil
	}
	filename := filepath.Join(source, filepath.FromSlash(pythonlock.PackagePath))
	raw, err := os.ReadFile(filename)
	if err != nil {
		return false, fmt.Errorf("Python 插件缺少 %s: %w", pythonlock.PackagePath, err)
	}
	summary, err := pythonlock.Inspect(raw)
	if err != nil {
		return false, err
	}
	if err := pythonlock.ValidateManifestRequirements(execution.Requirements, summary); err != nil {
		return false, err
	}
	referenced := make(map[string]struct{}, len(summary.Wheels))
	for _, wheel := range summary.Wheels {
		wheelFile := filepath.Join(source, filepath.FromSlash(wheel.PackagePath))
		info, err := os.Stat(wheelFile)
		if err != nil || !info.Mode().IsRegular() || info.Size() != wheel.Size {
			return false, fmt.Errorf("pylock.toml wheel 缺失或大小不一致: %s", wheel.PackagePath)
		}
		content, err := os.ReadFile(wheelFile)
		if err != nil {
			return false, err
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != wheel.SHA256 {
			return false, fmt.Errorf("pylock.toml wheel 摘要不一致: %s", wheel.PackagePath)
		}
		referenced[wheel.Name] = struct{}{}
	}
	wheelDirectory := filepath.Join(source, filepath.FromSlash(strings.TrimSuffix(pythonlock.WheelPathPrefix, "/")))
	entries, err := os.ReadDir(wheelDirectory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return false, fmt.Errorf("Python wheel 目录不得包含子目录: %s", entry.Name())
		}
		if _, exists := referenced[entry.Name()]; !exists {
			return false, fmt.Errorf("Python wheel 目录包含 pylock.toml 未引用文件: %s", entry.Name())
		}
	}
	bound := &pluginv1.SupplyChainDocument{Format: pythonlock.Format, SpecVersion: pythonlock.SpecVersion, Path: pythonlock.PackagePath, SHA256: summary.SHA256}
	if declared != nil {
		if *declared != *bound {
			return false, errors.New("源码清单 pythonLock 声明与实际 pylock.toml 不一致")
		}
		return false, nil
	}
	if manifest.SupplyChain == nil {
		manifest.SupplyChain = &pluginv1.SupplyChain{}
	}
	manifest.SupplyChain.PythonLock = bound
	return true, nil
}
