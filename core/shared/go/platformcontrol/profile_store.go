// Package platformcontrol owns the trusted, non-business mechanisms required
// to bootstrap and bind the platform control database. Database drivers and UI
// workflows remain outside this package behind ports.
package platformcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
)

var ErrGenerationConflict = errors.New("platform control profile generation conflict")

type ProfileStore interface {
	Load(context.Context) (*platformcontrolv1.Profile, error)
	Commit(context.Context, platformcontrolv1.Profile, uint64) error
}

type FileProfileStore struct {
	Path string
	mu   sync.Mutex
}

func (s *FileProfileStore) Load(_ context.Context) (*platformcontrolv1.Profile, error) {
	if s == nil || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path {
		return nil, errors.New("Platform Control Profile 路径必须是规范绝对路径")
	}
	info, err := os.Lstat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("Platform Control Profile 必须是 owner-only 普通文件")
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, err
	}
	profile, err := platformcontrolv1.ParseProfile(raw)
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *FileProfileStore) Commit(ctx context.Context, candidate platformcontrolv1.Profile, expected uint64) error {
	if s == nil || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path {
		return errors.New("Platform Control Profile 路径必须是规范绝对路径")
	}
	if err := platformcontrolv1.ValidateProfile(candidate); err != nil {
		return err
	}
	if candidate.Generation != expected+1 {
		return ErrGenerationConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.Load(ctx)
	if err != nil {
		return err
	}
	if current == nil && expected != 0 || current != nil && current.Generation != expected {
		return ErrGenerationConflict
	}
	raw, err := json.Marshal(candidate)
	if err != nil {
		return err
	}
	return writeOwnerFile(s.Path, append(raw, '\n'))
}

func writeOwnerFile(path string, raw []byte) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".platform-control-profile-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("同步 Platform Control Profile 目录: %w", err)
	}
	return nil
}
