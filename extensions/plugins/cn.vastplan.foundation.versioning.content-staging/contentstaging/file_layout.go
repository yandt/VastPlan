package contentstaging

// vastplan:local-file-boundary provider-private

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (p *FileProvider) tenantChild(scope Scope, child string, create bool) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	hash := pathDigest(scope.TenantID)
	components := []string{"tenants", hash, child}
	if !create {
		if err := validateExistingDirectoryChain(p.root, components); err != nil {
			return "", err
		}
		return filepath.Join(append([]string{p.root}, components...)...), nil
	}
	current := p.root
	var err error
	for _, component := range components {
		current, err = secureChildDirectory(current, component)
		if err != nil {
			return "", err
		}
	}
	return current, nil
}

func (p *FileProvider) tenantObjectDirectory(scope Scope, create bool) (string, error) {
	base, err := p.tenantChild(scope, "objects", create)
	if err != nil {
		return "", err
	}
	if create {
		return secureChildDirectory(base, "sha256")
	}
	if err := validateExistingDirectoryChain(base, []string{"sha256"}); err != nil {
		return "", err
	}
	return filepath.Join(base, "sha256"), nil
}

func ensurePrivateRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
		return "", errors.New("Content Staging File Provider root 必须是绝对路径")
	}
	root = filepath.Clean(root)
	if root == filepath.Clean(string(filepath.Separator)) || root == filepath.VolumeName(root)+string(filepath.Separator) {
		return "", errors.New("Content Staging File Provider root 不能是文件系统根目录")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	if err := validatePrivateDirectory(root); err != nil {
		return "", err
	}
	return root, nil
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("Content Staging 目录 %s 必须是权限 0700 的真实目录", path)
	}
	return nil
}

func secureChildDirectory(parent, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return "", errors.New("Content Staging 子目录名无效")
	}
	if err := validatePrivateDirectory(parent); err != nil {
		return "", err
	}
	child := filepath.Join(parent, name)
	if err := os.Mkdir(child, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if err := validatePrivateDirectory(child); err != nil {
		return "", err
	}
	return child, nil
}

func validateExistingDirectoryChain(root string, components []string) error {
	current := root
	if err := validatePrivateDirectory(current); err != nil {
		return err
	}
	for _, component := range components {
		current = filepath.Join(current, component)
		if err := validatePrivateDirectory(current); err != nil {
			return err
		}
	}
	return nil
}

func pathDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func isLowerHex(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}
