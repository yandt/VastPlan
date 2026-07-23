// Package artifactreport stores scanner-native reports as private immutable
// content-addressed evidence. The control plane carries only their digests.
package artifactreport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const MaxBytes int64 = 64 << 20

type Archive struct{ root string }

func (a *Archive) Ready() error { return a.secureRoot() }

func New(root string) (*Archive, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("安全评估报告归档根必须是规范绝对路径")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	archive := &Archive{root: root}
	if err := archive.secureRoot(); err != nil {
		return nil, err
	}
	return archive, nil
}

func (a *Archive) Put(digest string, raw []byte) error {
	if a == nil || !validDigest(digest) || len(raw) == 0 || int64(len(raw)) > MaxBytes || digestBytes(raw) != digest {
		return errors.New("安全评估报告归档参数无效")
	}
	if err := a.secureRoot(); err != nil {
		return err
	}
	target := a.path(digest)
	if _, err := os.Lstat(target); err == nil {
		return a.Require(digest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(a.root, ".report-candidate-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
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
	// Link publishes without replacing an existing digest. Both paths are on
	// one provisioned volume, so readers see either no report or complete bytes.
	if err := os.Link(temporaryPath, target); err != nil {
		if errors.Is(err, os.ErrExist) {
			return a.Require(digest)
		}
		return fmt.Errorf("原子发布安全评估报告: %w", err)
	}
	if err := syncDirectory(a.root); err != nil {
		return err
	}
	return a.Require(digest)
}

func (a *Archive) Require(digests ...string) error {
	if a == nil || len(digests) == 0 {
		return errors.New("安全评估报告引用不能为空")
	}
	if err := a.secureRoot(); err != nil {
		return err
	}
	for _, digest := range digests {
		if !validDigest(digest) {
			return errors.New("安全评估报告摘要无效")
		}
		actual, err := hashPrivateRegularFile(a.path(digest))
		if err != nil {
			return err
		}
		if actual != digest {
			return errors.New("安全评估报告内容与摘要引用不一致")
		}
	}
	return nil
}

func (a *Archive) Read(digest string) ([]byte, error) {
	if a == nil || !validDigest(digest) {
		return nil, errors.New("安全评估报告摘要无效")
	}
	if err := a.secureRoot(); err != nil {
		return nil, err
	}
	raw, actual, err := readPrivateRegularFile(a.path(digest))
	if err != nil {
		return nil, err
	}
	if actual != digest {
		return nil, errors.New("安全评估报告内容与摘要引用不一致")
	}
	return raw, nil
}

func (a *Archive) secureRoot() error {
	if a == nil || a.root == "" {
		return errors.New("安全评估报告归档未初始化")
	}
	info, err := os.Lstat(a.root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("安全评估报告归档根必须仅属主可访问且非符号链接")
	}
	return nil
}

func (a *Archive) path(digest string) string { return filepath.Join(a.root, digest+".json") }

func hashPrivateRegularFile(path string) (string, error) {
	_, digest, err := readPrivateRegularFile(path)
	return digest, err
}

func readPrivateRegularFile(path string) ([]byte, string, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm()&0o077 != 0 || before.Size() <= 0 || before.Size() > MaxBytes {
		return nil, "", errors.New("安全评估报告缺失、不是私有普通文件或大小超限")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Size() != before.Size() {
		return nil, "", errors.New("安全评估报告打开期间身份发生变化")
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil || int64(len(raw)) != before.Size() || int64(len(raw)) > MaxBytes {
		return nil, "", errors.New("读取安全评估报告失败或大小漂移")
	}
	return raw, digestBytes(raw), nil
}

func validDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size && value == hex.EncodeToString(raw)
}

func digestBytes(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
