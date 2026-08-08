package contentstaging

// vastplan:local-file-boundary provider-private

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func readPrivateJSON(path string, limit int64, target any) error {
	file, err := openPrivateRegular(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > limit {
		return errors.New("Content Staging 状态文件大小无效")
	}
	decoder := json.NewDecoder(io.LimitReader(file, limit+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Content Staging 状态文件包含多余 JSON")
	}
	return nil
}

func writeAtomicJSON(directory, prefix, name string, value any) error {
	if err := validatePrivateDirectory(directory); err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, prefix)
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := io.Copy(temporary, bytes.NewReader(raw)); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := errors.Join(temporary.Sync(), temporary.Close()); err != nil {
		return err
	}
	if err := rejectNonRegularTarget(filepath.Join(directory, name)); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, filepath.Join(directory, name)); err != nil {
		return err
	}
	return syncDirectory(directory)
}

func rejectNonRegularTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("Content Staging 目标不是私有普通文件")
	}
	return nil
}

func openPrivateRegular(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("Content Staging 文件必须是权限 0600 的普通文件")
	}
	return os.Open(path)
}

func removeRegularFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("拒绝删除非私有普通文件")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func verifyFile(path, expectedDigest string, expectedSize int64) error {
	file, err := openPrivateRegular(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != expectedSize {
		return errors.New("Content Staging 文件大小不匹配")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedDigest {
		return errors.New("Content Staging 文件摘要不匹配")
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
