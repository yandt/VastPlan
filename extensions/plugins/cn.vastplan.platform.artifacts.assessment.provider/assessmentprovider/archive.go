package assessmentprovider

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func archiveReport(root, digest string, raw []byte) error {
	actual := sha256.Sum256(raw)
	if len(digest) != 64 || len(raw) == 0 || hex.EncodeToString(actual[:]) != digest {
		return errors.New("安全评估报告归档参数无效")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("安全评估报告目录必须仅属主可访问且非符号链接")
	}
	path := filepath.Join(root, digest+".json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return errors.New("安全评估报告摘要路径不是私有普通文件")
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(existing, raw) {
			return errors.New("安全评估报告摘要路径发生内容冲突")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(raw); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("关闭安全评估报告: %w", err)
	}
	return nil
}
