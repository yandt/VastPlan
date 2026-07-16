//go:build unix

package nodeagent

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ProcessLock 使用内核 advisory lock 保护一个 Node Agent 运行目录。
// 进程异常退出时锁由操作系统自动释放，不会遗留需要人工删除的假死锁文件。
type ProcessLock struct {
	file *os.File
}

func AcquireProcessLock(filename string) (*ProcessLock, error) {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return nil, fmt.Errorf("创建锁目录: %w", err)
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开 Node Agent 锁: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("已有 Node Agent 持有锁 %s: %w", filename, err)
	}
	if err := file.Truncate(0); err == nil {
		_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
		_ = file.Sync()
	}
	return &ProcessLock{file: file}, nil
}

func (l *ProcessLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
