package artifactassessmentprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var trivyDatabaseFiles = []string{"db/metadata.json", "db/trivy.db"}

func validDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size
}

func databaseSnapshotDigest(root string) (string, error) {
	hash := sha256.New()
	for _, relative := range trivyDatabaseFiles {
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			return "", fmt.Errorf("Trivy 数据库文件不存在、不是普通文件或为空: %s", relative)
		}
		if _, err := io.WriteString(hash, relative+"\x00"); err != nil {
			return "", err
		}
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if _, err := io.WriteString(hash, "\x00"); err != nil {
			return "", err
		}
	}
	result := hex.EncodeToString(hash.Sum(nil))
	if !validDigest(result) {
		return "", errors.New("Trivy 数据库摘要计算失败")
	}
	return result, nil
}

// TrivyDatabaseRevision returns the content identity expected by TrivyConfig.
// Database updaters compute it after atomically preparing a snapshot and
// publish the immutable value with the Provider configuration.
func TrivyDatabaseRevision(root string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", errors.New("Trivy cacheDirectory 必须是规范绝对路径")
	}
	return databaseSnapshotDigest(root)
}
