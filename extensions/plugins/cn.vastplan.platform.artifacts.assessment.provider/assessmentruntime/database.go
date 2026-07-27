package assessmentruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/trivydatabase"
)

var trivyDatabaseFiles = []string{"db/metadata.json", "db/trivy.db"}

func databaseSnapshotDigest(root string) (string, error) {
	files := make([]*os.File, 0, len(trivyDatabaseFiles))
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	for _, relative := range trivyDatabaseFiles {
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
			return "", fmt.Errorf("Trivy 数据库文件不存在、不是普通文件或为空: %s", relative)
		}
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		files = append(files, file)
	}
	return trivydatabase.Revision(files[0], files[1])
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
