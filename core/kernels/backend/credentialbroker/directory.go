// Package credentialbroker contains trusted kernel-side credential providers.
package credentialbroker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
)

const maxCredentialBytes = 4 << 20

var safeComponent = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,159}$`)

// Directory resolves <root>/<tenant>/<credential-name>. It is suitable for
// enterprise secret mounts and bootstrap environments; files must be 0600 and
// directories must not be writable by group/other.
type Directory struct{ root string }

func NewDirectory(root string) (*Directory, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("credential root 必须是规范绝对路径")
	}
	if err := secureDirectory(root); err != nil {
		return nil, fmt.Errorf("credential root: %w", err)
	}
	return &Directory{root: root}, nil
}

type material []byte

func (m material) Bytes() []byte { return m }

func (d *Directory) WithCredential(ctx context.Context, scope kernelspi.Scope, ref kernelspi.CredentialRef, use func(kernelspi.CredentialMaterial) error) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if ctx == nil || use == nil || ref.Scope != "tenant" || !safeComponent.MatchString(scope.TenantID) || !safeComponent.MatchString(ref.Name) {
		return errors.New("credential scope 或引用无效")
	}
	if err := secureDirectory(d.root); err != nil {
		return fmt.Errorf("credential root: %w", err)
	}
	tenantRoot := filepath.Join(d.root, scope.TenantID)
	if err := secureDirectory(tenantRoot); err != nil {
		return fmt.Errorf("credential tenant root: %w", err)
	}
	filename := filepath.Join(tenantRoot, ref.Name)
	info, err := os.Lstat(filename)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > maxCredentialBytes {
		return errors.New("credential material 必须是仅属主可访问且大小受限的普通文件")
	}
	handle, err := os.Open(filename)
	if err != nil {
		return err
	}
	opened, statErr := handle.Stat()
	if statErr != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		_ = handle.Close()
		return errors.New("credential material 在读取期间发生替换")
	}
	raw, readErr := io.ReadAll(io.LimitReader(handle, maxCredentialBytes+1))
	closeErr := handle.Close()
	if readErr != nil || closeErr != nil || len(raw) == 0 || len(raw) > maxCredentialBytes {
		for i := range raw {
			raw[i] = 0
		}
		return errors.New("读取 credential material 失败")
	}
	defer func() {
		for i := range raw {
			raw[i] = 0
		}
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return use(material(raw))
	}
}

func secureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return errors.New("目录不存在、是符号链接或可被 group/other 写入")
	}
	return nil
}
