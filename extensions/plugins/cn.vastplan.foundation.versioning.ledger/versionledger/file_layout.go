package versionledger

// vastplan:local-file-boundary provider-private

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

const (
	fileProviderFormatVersion = 2
	maxVersionFileBytes       = versioningv1.MaxContentBytes + 64<<10
	maxHeadFileBytes          = 16 << 10
)

type fileVersionEnvelope struct {
	FormatVersion   int                        `json:"formatVersion"`
	CandidateDigest string                     `json:"candidateDigest"`
	Record          versioningv1.VersionRecord `json:"record"`
}

type fileHeadEnvelope struct {
	FormatVersion int               `json:"formatVersion"`
	Head          versioningv1.Head `json:"head"`
	Deleted       bool              `json:"deleted,omitempty"`
}

type fileTagEnvelope struct {
	FormatVersion int              `json:"formatVersion"`
	Tag           versioningv1.Tag `json:"tag"`
}

type loadedFileStream struct {
	sequence uint64
	versions map[string]versioningv1.VersionRecord
}

func ensureProviderRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
		return "", errors.New("File Version Provider root 必须是绝对路径")
	}
	root = filepath.Clean(root)
	if root == filepath.Clean(string(filepath.Separator)) || root == filepath.VolumeName(root)+string(filepath.Separator) {
		return "", errors.New("File Version Provider root 不能是文件系统根目录")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("创建 File Version Provider root: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("File Version Provider root 必须是真实目录")
	}
	if err := validatePrivateDirectory(root); err != nil {
		return "", err
	}
	return root, nil
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("Version Provider 目录 %s 必须是权限 0700 的真实目录", path)
	}
	return nil
}

func secureChildDirectory(parent, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return "", errors.New("Version Provider 子目录名无效")
	}
	if err := validatePrivateDirectory(parent); err != nil {
		return "", err
	}
	child := filepath.Join(parent, name)
	if err := os.Mkdir(child, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if err := validatePrivateDirectory(child); err != nil {
		return "", err
	}
	return child, nil
}

func (p *FileProvider) streamDirectory(scope Scope, stream versioningv1.StreamKey, create bool) (string, error) {
	tenantHash := pathDigest(scope.TenantID)
	streamHash := pathDigest(stream.Namespace + "\x00" + stream.StreamID)
	path := filepath.Join(p.root, "tenants", tenantHash, "streams", streamHash)
	if !create {
		if err := validateExistingDirectoryChain(p.root, []string{"tenants", tenantHash, "streams", streamHash}); err != nil {
			return "", err
		}
		return path, nil
	}
	current := p.root
	var err error
	for _, component := range []string{"tenants", tenantHash, "streams", streamHash} {
		current, err = secureChildDirectory(current, component)
		if err != nil {
			return "", err
		}
	}
	return current, nil
}

func validateExistingDirectoryChain(root string, components []string) error {
	current := root
	if err := validatePrivateDirectory(current); err != nil {
		return err
	}
	for _, component := range components {
		current = filepath.Join(current, component)
		if err := validatePrivateDirectory(current); err != nil {
			return err
		}
	}
	return nil
}

func pathDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func loadFileStream(streamDir string, stream versioningv1.StreamKey) (loadedFileStream, error) {
	loaded := loadedFileStream{versions: map[string]versioningv1.VersionRecord{}}
	versionsDir := filepath.Join(streamDir, "versions")
	entries, err := os.ReadDir(versionsDir)
	if errors.Is(err, os.ErrNotExist) {
		return loaded, nil
	}
	if err != nil {
		return loaded, err
	}
	if err := validatePrivateDirectory(versionsDir); err != nil {
		return loaded, err
	}
	sequences := map[uint64]struct{}{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".version-") {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			return loaded, fmt.Errorf("versions 目录包含未知条目 %q", name)
		}
		versionID := strings.TrimSuffix(name, ".json")
		if len(versionID) != 64 {
			return loaded, fmt.Errorf("版本文件名无效 %q", name)
		}
		var envelope fileVersionEnvelope
		if err := readPrivateJSON(filepath.Join(versionsDir, name), maxVersionFileBytes, &envelope); err != nil {
			return loaded, err
		}
		record := envelope.Record
		if envelope.FormatVersion != fileProviderFormatVersion || record.Ref.VersionID != versionID || record.Ref.Stream != stream {
			return loaded, fmt.Errorf("版本文件 %q 身份不一致", name)
		}
		if err := versioningv1.ValidateVersionRecord(record); err != nil {
			return loaded, fmt.Errorf("版本文件 %q 损坏: %w", name, err)
		}
		digest, err := candidateDigest(candidateFromRecord(record))
		if err != nil || digest != envelope.CandidateDigest {
			return loaded, fmt.Errorf("版本文件 %q 候选摘要不匹配", name)
		}
		if _, duplicate := loaded.versions[versionID]; duplicate {
			return loaded, fmt.Errorf("版本 %q 重复", versionID)
		}
		if _, duplicate := sequences[record.Ref.Sequence]; duplicate {
			return loaded, fmt.Errorf("版本 sequence %d 重复", record.Ref.Sequence)
		}
		loaded.versions[versionID] = cloneRecord(record)
		sequences[record.Ref.Sequence] = struct{}{}
		if record.Ref.Sequence > loaded.sequence {
			loaded.sequence = record.Ref.Sequence
		}
	}
	for _, record := range loaded.versions {
		if len(record.Parents) == 0 {
			if record.Ref.Sequence != 1 {
				return loaded, errors.New("非首个版本缺少父节点")
			}
			continue
		}
		for _, parentRef := range record.Parents {
			parent, ok := loaded.versions[parentRef.VersionID]
			if !ok || parent.Ref != parentRef {
				return loaded, errors.New("版本父链不闭合")
			}
		}
	}
	return loaded, nil
}

func readPrivateJSON(path string, limit int64, target any) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > limit {
		return fmt.Errorf("Version Provider 文件 %s 类型、权限或大小无效", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, limit+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Version Provider 文件包含多余 JSON")
	}
	return nil
}

func writeAtomicJSON(directory, prefix, finalName string, value any) error {
	return writeJSONFile(directory, prefix, finalName, value, false)
}

func writeCreateOnlyJSON(directory, prefix, finalName string, value any) error {
	return writeJSONFile(directory, prefix, finalName, value, true)
}

func writeJSONFile(directory, prefix, finalName string, value any, createOnly bool) error {
	if err := validatePrivateDirectory(directory); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, prefix)
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, bytes.NewReader(raw)); err != nil {
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
	finalPath := filepath.Join(directory, finalName)
	if createOnly {
		if err := os.Link(temporaryName, finalPath); err != nil {
			return err
		}
	} else if err := os.Rename(temporaryName, finalPath); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func syncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	syncErr := handle.Sync()
	closeErr := handle.Close()
	return errors.Join(syncErr, closeErr)
}
