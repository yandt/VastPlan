package localtest

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
)

func ReadTokenFile(filename string) (string, error) {
	if filename == "" || !filepath.IsAbs(filename) || filepath.Clean(filename) != filename {
		return "", errors.New("local-test token file 必须是规范绝对路径")
	}
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("local-test token file 必须是 owner-only 普通文件")
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(raw))
	if len(value) < minTokenBytes {
		return "", errors.New("local-test token 至少需要 32 字节")
	}
	return value, nil
}

// Listen creates the protocol's only supported listener. The caller owns the
// listener and socket lifecycle; an existing path is never removed implicitly.
func Listen(profile artifactrepositoryv1.Profile) (net.Listener, error) {
	profile, err := artifactrepositoryv1.ValidateProfile(profile)
	if err != nil {
		return nil, err
	}
	if profile.Protocol != artifactrepositoryv1.ProtocolLocalTest {
		return nil, fmt.Errorf("协议 %s 不能创建 local-test listener", profile.Protocol)
	}
	path, err := socketPath(profile)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil {
		return nil, fmt.Errorf("检查 local-test socket 目录: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, errorsPrivateDirectory(parent)
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("local-test socket 路径已存在，拒绝隐式覆盖: %s", path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("检查 local-test socket: %w", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("监听 local-test socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("收紧 local-test socket 权限: %w", err)
	}
	return listener, nil
}

func socketPath(profile artifactrepositoryv1.Profile) (string, error) {
	parsed, err := url.Parse(profile.Endpoint)
	if err != nil || parsed.Scheme != "unix" || parsed.Path == "" {
		return "", fmt.Errorf("local-test endpoint 不是 Unix Socket: %s", profile.Endpoint)
	}
	return parsed.Path, nil
}

func errorsPrivateDirectory(path string) error {
	return fmt.Errorf("local-test socket 目录必须是无符号链接且 group/other 无权限的私有目录: %s", path)
}
