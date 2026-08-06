package authorizationpolicy

// vastplan:local-file-boundary bootstrap-root
// FileBootstrapStateReader is read only by the one-time Bootstrap import path.
// Runtime policy state is owned by the fenced Shared State Store.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

type Store interface {
	Load() (State, error)
	CompareAndSwap(expected uint64, next State) (State, error)
}

// BootstrapStateReader loads owner-controlled initial policy state. Runtime
// policy mutations remain confined to the Shared State-backed Store port.
type BootstrapStateReader interface {
	Load() (State, error)
}

var ErrStoreUnavailable = errors.New("Authorization Policy Store 不可用")

// FileBootstrapStateReader is intentionally load-only. Bootstrap files are
// import input, never a writable Authorization Policy runtime Store.
type FileBootstrapStateReader struct{ Path string }

func (s *FileBootstrapStateReader) Load() (State, error) {
	if err := validateStatePath(s.Path); err != nil {
		return State{}, err
	}
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Version: stateVersion, Roles: []RoleRevision{}, Bindings: []BindingRevision{}, Revocations: []authorizationv1.Revocation{}, Audit: []AuditEvent{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	info, err := os.Lstat(s.Path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return State{}, errors.New("Authorization Policy 状态必须是 owner-only 普通文件")
	}
	var state State
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("解析 Authorization Policy 状态: %w", err)
	}
	if state.Version != stateVersion {
		return State{}, errors.New("Authorization Policy 状态版本无效")
	}
	return state, nil
}

func validateStatePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Ext(path) != ".json" {
		return errors.New("Authorization Policy Store 必须是规范绝对 JSON 路径")
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("Authorization Policy Store 目录必须不可被 group/other 写入")
	}
	return nil
}
