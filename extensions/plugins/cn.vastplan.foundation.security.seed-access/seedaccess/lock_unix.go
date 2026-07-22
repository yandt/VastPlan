//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package seedaccess

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

type stateLock struct{ file *os.File }

func acquireStateLock(path string) (*stateLock, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		_ = file.Close()
		return nil, errors.New("Seed Access lock 必须是 owner-only 普通文件")
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &stateLock{file: file}, nil
}

func (l *stateLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return errors.Join(unix.Flock(int(l.file.Fd()), unix.LOCK_UN), l.file.Close())
}
