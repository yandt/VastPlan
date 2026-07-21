//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package bootstrapupgrade

import (
	"errors"
	"os"
)

type inventoryLock struct {
	file *os.File
	path string
}

func acquireInventoryLock(path string) (*inventoryLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errors.New("当前平台无法取得 Bootstrap Inventory 独占锁")
	}
	return &inventoryLock{file: file, path: path}, nil
}

func (l *inventoryLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return errors.Join(l.file.Close(), os.Remove(l.path))
}
