package pluginservice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

var retirementIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

const (
	RetirementMissing     = "missing"
	RetirementActive      = "active"
	RetirementQuarantined = "quarantined"
)

// InspectRetirement reports the physical location of one exact artifact
// without exposing repository paths to the caller.
func (r *Repository) InspectRetirement(ref pluginv1.ArtifactRef, expectedSHA256, retirementID string) (string, error) {
	if r == nil {
		return "", errors.New("制品仓库未配置")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inspectRetirement(ref, expectedSHA256, retirementID)
}

// QuarantineArtifact atomically removes an artifact directory from the active
// namespace. It is idempotent for the same retirement ID and exact SHA-256.
func (r *Repository) QuarantineArtifact(ref pluginv1.ArtifactRef, expectedSHA256, retirementID string) error {
	if r == nil {
		return errors.New("制品仓库未配置")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	location, err := r.inspectRetirement(ref, expectedSHA256, retirementID)
	if err != nil {
		return err
	}
	if location == RetirementQuarantined {
		return nil
	}
	if location != RetirementActive {
		return errors.New("待隔离制品不存在")
	}
	active, err := r.artifactDir(ref)
	if err != nil {
		return err
	}
	quarantine, err := r.quarantineDir(ref, retirementID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(quarantine), 0o700); err != nil {
		return err
	}
	if err := os.Rename(active, quarantine); err != nil {
		return fmt.Errorf("原子隔离制品: %w", err)
	}
	return nil
}

// SweepArtifact permanently removes bytes that were already quarantined.
// The caller owns the grace-period and reference revalidation policy.
func (r *Repository) SweepArtifact(ref pluginv1.ArtifactRef, expectedSHA256, retirementID string) error {
	if r == nil {
		return errors.New("制品仓库未配置")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(expectedSHA256) != 64 || !retirementIDPattern.MatchString(retirementID) {
		return errors.New("制品 retirement 身份无效")
	}
	active, err := r.artifactDir(ref)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(active); err == nil {
		return errors.New("活动制品不能直接 sweep")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	quarantine, err := r.quarantineDir(ref, retirementID)
	if err != nil {
		return err
	}
	info, err := os.Lstat(quarantine)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("制品隔离区不是普通目录")
	}
	// A prior RemoveAll may have deleted artifact.json before the process
	// crashed. If metadata still exists, verify it; otherwise the durable GC
	// record and derived path are sufficient to resume removal.
	if artifact, readErr := readArtifact(filepath.Join(quarantine, "artifact.json")); readErr == nil {
		if artifact.PluginID != ref.PluginID || artifact.Version != ref.Version || artifact.Channel != ref.Channel || artifact.SHA256 != expectedSHA256 {
			return errors.New("制品隔离区与精确 retirement 身份不一致")
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := os.RemoveAll(quarantine); err != nil {
		return fmt.Errorf("清扫隔离制品: %w", err)
	}
	return nil
}

func (r *Repository) inspectRetirement(ref pluginv1.ArtifactRef, expectedSHA256, retirementID string) (string, error) {
	if len(expectedSHA256) != 64 || !retirementIDPattern.MatchString(retirementID) {
		return "", errors.New("制品 retirement 身份无效")
	}
	active, err := r.artifactDir(ref)
	if err != nil {
		return "", err
	}
	quarantine, err := r.quarantineDir(ref, retirementID)
	if err != nil {
		return "", err
	}
	activeOK, err := exactArtifactAt(active, ref, expectedSHA256)
	if err != nil {
		return "", err
	}
	quarantineOK, err := exactArtifactAt(quarantine, ref, expectedSHA256)
	if err != nil {
		return "", err
	}
	if activeOK && quarantineOK {
		return "", errors.New("活动区与隔离区同时存在同一制品")
	}
	if activeOK {
		return RetirementActive, nil
	}
	if quarantineOK {
		return RetirementQuarantined, nil
	}
	return RetirementMissing, nil
}

func (r *Repository) quarantineDir(ref pluginv1.ArtifactRef, retirementID string) (string, error) {
	if !retirementIDPattern.MatchString(retirementID) {
		return "", errors.New("retirement ID 必须是 64 位小写摘要")
	}
	active, err := r.artifactDir(ref)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(filepath.Join(r.root, "artifacts"), active)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return "", errors.New("制品隔离路径无效")
	}
	return filepath.Join(r.root, "quarantine", "artifacts", retirementID, relative), nil
}

func exactArtifactAt(directory string, ref pluginv1.ArtifactRef, expectedSHA256 string) (bool, error) {
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, errors.New("制品 retirement 目录不是普通目录")
	}
	artifact, err := readArtifact(filepath.Join(directory, "artifact.json"))
	if err != nil {
		return false, err
	}
	if artifact.PluginID != ref.PluginID || artifact.Version != ref.Version || artifact.Channel != ref.Channel || artifact.SHA256 != expectedSHA256 {
		return false, errors.New("制品 retirement 目录与精确身份不一致")
	}
	return true, nil
}
