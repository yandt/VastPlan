package bootstrapupgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

type FileInventoryStore struct{ Path string }

func (s FileInventoryStore) Load() (bootstrapinventory.Inventory, error) {
	return bootstrapinventory.ParseFile(s.Path)
}

func (s FileInventoryStore) Update(expectedGeneration uint64, next bootstrapinventory.Inventory) (bootstrapinventory.Inventory, error) {
	if !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path || filepath.Ext(s.Path) != ".json" {
		return bootstrapinventory.Inventory{}, errors.New("可升级 Bootstrap Inventory 必须是规范绝对 JSON 路径")
	}
	directory := filepath.Dir(s.Path)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return bootstrapinventory.Inventory{}, errors.New("Bootstrap Inventory 目录必须是不可被 group/other 写入的普通目录")
	}
	lock, err := acquireInventoryLock(s.Path + ".lock")
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	defer lock.Close()
	current, err := bootstrapinventory.ParseFile(s.Path)
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	normalized, err := bootstrapinventory.Normalize(next)
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	// A rename may have committed before the caller observed a directory-fsync
	// error. Treat an exact retry as success instead of turning it into a false
	// CAS conflict.
	if current.Generation == normalized.Generation && reflect.DeepEqual(current, normalized) {
		return current, nil
	}
	if current.Generation != expectedGeneration || next.Generation != expectedGeneration+1 || next.RepositoryID != current.RepositoryID {
		return bootstrapinventory.Inventory{}, fmt.Errorf("Bootstrap Inventory CAS 冲突: expected=%d actual=%d next=%d", expectedGeneration, current.Generation, next.Generation)
	}
	raw, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	file, err := os.CreateTemp(directory, ".bootstrap-inventory-*")
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	temporary := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	if err := os.Rename(temporary, s.Path); err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	committed = true
	dir, err := os.Open(directory)
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	if err := errors.Join(dir.Sync(), dir.Close()); err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	return normalized, nil
}
