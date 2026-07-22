//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package seedaccess

import (
	"errors"
	"os"
)

type stateLock struct{ file *os.File }

func acquireStateLock(path string) (*stateLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	return &stateLock{file: file}, nil
}

func (l *stateLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return errors.Join(l.file.Close(), os.Remove(l.file.Name()))
}
