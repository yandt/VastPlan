package artifactassessmentprovider

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

func extractPackage(packageBytes []byte, destination string) error {
	// InspectPackage performs the authoritative entry count, expanded-size,
	// duplicate-path and ordinary-file-only validation before disk writes.
	if _, _, err := artifacttrust.InspectPackage(packageBytes); err != nil {
		return fmt.Errorf("预检安全评估制品: %w", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(packageBytes))
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return errors.New("安全评估制品包含越界路径")
		}
		path := filepath.Join(destination, name)
		if !strings.HasPrefix(path, destination+string(filepath.Separator)) {
			return errors.New("安全评估制品路径逃逸工作目录")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		written, copyErr := io.CopyN(file, reader, header.Size)
		closeErr := file.Close()
		if copyErr != nil || written != header.Size {
			return fmt.Errorf("展开安全评估制品文件 %s: %w", name, copyErr)
		}
		if closeErr != nil {
			return closeErr
		}
	}
}
