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

// ErrCommittedButUnsynced reports that a profile reached its final path through
// the atomic rename but the parent directory fsync did not complete. The
// profile is already durably visible, so callers must treat it as committed:
// rolling back the secret it references or restoring the unconfigured state
// would tear a commit that actually succeeded.
var ErrCommittedButUnsynced = errors.New("platform control profile committed but directory sync failed")

type ProfileStore interface {
	// Exists reports whether a profile has ever been committed, independent of
	// whether its content can currently be read or parsed. The durable fact
	// that a platform was configured must outlive a corrupt or unreadable
	// profile, so this is the signal that makes the provider requirement
	// permanent.
	Exists(context.Context) (bool, error)
	Load(context.Context) (*platformcontrolv1.Profile, error)
	Commit(context.Context, platformcontrolv1.Profile, uint64) error
}

type FileProfileStore struct {
	Path string
	mu   sync.Mutex
}

func (s *FileProfileStore) Exists(_ context.Context) (bool, error) {
	if s == nil || !filepath.IsAbs(s.Path) || filepath.Clean(s.Path) != s.Path {
		return false, errors.New("Platform Control Profile 路径必须是规范绝对路径")
	}
	if _, err := os.Lstat(s.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
	// Past this point the profile is durably visible at its final path. A
	// directory fsync failure may still lose the directory entry on a crash,
	// but it must never be reported as a failed commit: the caller would then
	// roll back the secret this profile already references and restore the
	// unconfigured state, tearing a commit that succeeded.
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("%w: 同步 Platform Control Profile 目录: %w", ErrCommittedButUnsynced, err)
	}
	return nil
}
