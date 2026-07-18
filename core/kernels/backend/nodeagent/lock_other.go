//go:build !unix

package nodeagent

import (
	"fmt"
	"os"
	"path/filepath"
)

// ProcessLock 在非 Unix 平台使用独占创建作为保守回退。
type ProcessLock struct {
	file *os.File
	path string
}

func AcquireProcessLock(filename string) (*ProcessLock, error) {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return nil, fmt.Errorf("创建锁目录: %w", err)
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("已有 Node Agent 持有锁 %s: %w", filename, err)
	}
	_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
	return &ProcessLock{file: file, path: filename}, nil
}

func (l *ProcessLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	closeErr := l.file.Close()
	removeErr := os.Remove(l.path)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}
