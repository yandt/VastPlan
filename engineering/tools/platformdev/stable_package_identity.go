package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

const developmentStablePackageIdentitySchema = 1

type stablePackageIdentity struct {
	PluginID string `json:"pluginId"`
	Version  string `json:"version"`
	Channel  string `json:"channel"`
	Variant  string `json:"variant,omitempty"`
	SHA256   string `json:"sha256"`
}

type stablePackageIdentityLedger struct {
	Schema    int                     `json:"schema"`
	Artifacts []stablePackageIdentity `json:"artifacts"`
}

func stablePackageIdentityLedgerPath(root string) string {
	return filepath.Join(root, ".vastplan", "stable-package-identities.json")
}

// reconcileStablePackageIdentities records every stable exact ref observed in
// this worktree and rejects later bytes for the same ref. This catches a missed
// SemVer bump at build time, before an Activation or Deployment sees an
// ambiguous ref. The ledger intentionally lives outside dev-platform so
// --fresh cannot turn an immutable stable version into a mutable one.
func reconcileStablePackageIdentities(repositoryRoot, ledgerPath string) error {
	current, err := readStablePackageIdentities(repositoryRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ledgerPath), 0o700); err != nil {
		return fmt.Errorf("创建稳定制品身份目录: %w", err)
	}
	unlock, err := lockStablePackageIdentityLedger(ledgerPath + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	ledger, err := loadStablePackageIdentityLedger(ledgerPath)
	if err != nil {
		return err
	}
	known := make(map[string]stablePackageIdentity, len(ledger.Artifacts))
	for _, identity := range ledger.Artifacts {
		key := stablePackageIdentityKey(identity)
		if _, duplicate := known[key]; duplicate {
			return fmt.Errorf("稳定制品身份账本包含重复精确引用: %s@%s/%s", identity.PluginID, identity.Version, identity.Channel)
		}
		known[key] = identity
	}
	for _, identity := range current {
		key := stablePackageIdentityKey(identity)
		if previous, ok := known[key]; ok && previous.SHA256 != identity.SHA256 {
			return fmt.Errorf("稳定制品身份漂移: %s 已记录 sha256=%s，本次为 sha256=%s；stable 精确引用不可覆盖，请提升插件 SemVer 后重试",
				stablePackageIdentityLabel(identity), previous.SHA256, identity.SHA256)
		}
		known[key] = identity
	}
	merged := make([]stablePackageIdentity, 0, len(known))
	for _, identity := range known {
		merged = append(merged, identity)
	}
	sortStablePackageIdentities(merged)
	if stablePackageIdentitiesEqual(ledger.Artifacts, merged) {
		return nil
	}
	return writeStablePackageIdentityLedger(ledgerPath, stablePackageIdentityLedger{
		Schema: developmentStablePackageIdentitySchema, Artifacts: merged,
	})
}

func readStablePackageIdentities(repositoryRoot string) ([]stablePackageIdentity, error) {
	identities := make([]stablePackageIdentity, 0)
	err := filepath.WalkDir(filepath.Join(repositoryRoot, "artifacts"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "artifact.json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := pluginv1.ValidateArtifactMetadata(raw); err != nil {
			return fmt.Errorf("验证稳定制品元数据 %s: %w", path, err)
		}
		var artifact pluginservice.Artifact
		if err := json.Unmarshal(raw, &artifact); err != nil {
			return err
		}
		if artifact.Channel != "stable" {
			return nil
		}
		identity := stablePackageIdentity{
			PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel, SHA256: artifact.SHA256,
		}
		manifest, err := pluginv1.ParseManifest(artifact.Manifest)
		if err != nil {
			return fmt.Errorf("解析稳定制品清单 %s: %w", path, err)
		}
		if manifest.Execution != nil && manifest.Execution.Backend != nil && manifest.Execution.Backend.DynamicGo != nil {
			identity.Variant = manifest.Execution.Backend.DynamicGo.Fingerprint
		}
		identities = append(identities, identity)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("读取稳定制品身份: %w", err)
	}
	sortStablePackageIdentities(identities)
	return identities, nil
}

func loadStablePackageIdentityLedger(path string) (stablePackageIdentityLedger, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return stablePackageIdentityLedger{Schema: developmentStablePackageIdentitySchema}, nil
	}
	if err != nil {
		return stablePackageIdentityLedger{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return stablePackageIdentityLedger{}, errors.New("稳定制品身份账本必须是普通文件")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return stablePackageIdentityLedger{}, err
	}
	var ledger stablePackageIdentityLedger
	if err := json.Unmarshal(raw, &ledger); err != nil {
		return stablePackageIdentityLedger{}, fmt.Errorf("解析稳定制品身份账本: %w", err)
	}
	if ledger.Schema != developmentStablePackageIdentitySchema {
		return stablePackageIdentityLedger{}, fmt.Errorf("不支持的稳定制品身份账本 schema: %d", ledger.Schema)
	}
	for _, identity := range ledger.Artifacts {
		if err := validateStablePackageIdentity(identity); err != nil {
			return stablePackageIdentityLedger{}, err
		}
	}
	sortStablePackageIdentities(ledger.Artifacts)
	return ledger, nil
}

func validateStablePackageIdentity(identity stablePackageIdentity) error {
	if strings.TrimSpace(identity.PluginID) == "" || strings.TrimSpace(identity.Version) == "" || identity.Channel != "stable" {
		return errors.New("稳定制品身份账本包含非法精确引用")
	}
	decoded, err := hex.DecodeString(identity.SHA256)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("稳定制品身份 %s@%s 的 SHA-256 无效", identity.PluginID, identity.Version)
	}
	if identity.Variant != "" {
		decoded, err := hex.DecodeString(identity.Variant)
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("稳定制品身份 %s@%s 的 dynamic-go variant 无效", identity.PluginID, identity.Version)
		}
	}
	return nil
}

func lockStablePackageIdentityLedger(path string) (func(), error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("稳定制品身份锁文件不得是符号链接")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("打开稳定制品身份锁: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("锁定稳定制品身份账本: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func writeStablePackageIdentityLedger(path string, ledger stablePackageIdentityLedger) error {
	raw, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".stable-package-identities-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("原子提交稳定制品身份账本: %w", err)
	}
	return nil
}

func stablePackageIdentityKey(identity stablePackageIdentity) string {
	return identity.PluginID + "\x00" + identity.Version + "\x00" + identity.Channel + "\x00" + identity.Variant
}

func stablePackageIdentityLabel(identity stablePackageIdentity) string {
	label := identity.PluginID + "@" + identity.Version + "/" + identity.Channel
	if identity.Variant != "" {
		label += " variant=" + identity.Variant
	}
	return label
}

func sortStablePackageIdentities(identities []stablePackageIdentity) {
	sort.Slice(identities, func(i, j int) bool {
		return stablePackageIdentityKey(identities[i]) < stablePackageIdentityKey(identities[j])
	})
}

func stablePackageIdentitiesEqual(left, right []stablePackageIdentity) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
