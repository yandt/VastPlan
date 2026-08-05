package platformcontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	platformcontrolport "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
)

const managedSecretPrefix = "platform-control-password-"

type PreparedSecret interface {
	Ref() platformcontrolv1.SecretRef
	Source() platformcontrolport.SecretSource
	Commit() error
	Rollback() error
}

// SecretMaterialStore owns only host-managed bootstrap password files. It does
// not resolve arbitrary external references and never exposes stored bytes.
type SecretMaterialStore interface {
	Prepare(context.Context, uint64, []byte) (PreparedSecret, error)
	Reconcile(*platformcontrolv1.SecretRef) error
}

type FileSecretMaterialStore struct{ Root string }

func (s *FileSecretMaterialStore) Prepare(ctx context.Context, generation uint64, material []byte) (PreparedSecret, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if generation == 0 || len(material) == 0 || len(material) > platformcontrolv1.MaxSecretMaterialBytes {
		return nil, errors.New("Bootstrap password material 无效")
	}
	if err := s.ensureRoot(); err != nil {
		return nil, err
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("%sg%d-%s", managedSecretPrefix, generation, hex.EncodeToString(random))
	temporary, err := os.CreateTemp(s.Root, ".platform-control-password-candidate-")
	if err != nil {
		return nil, err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return nil, err
	}
	if _, err := temporary.Write(material); err != nil {
		return nil, err
	}
	if err := temporary.Sync(); err != nil {
		return nil, err
	}
	if err := temporary.Close(); err != nil {
		return nil, err
	}
	committed = true
	return &filePreparedSecret{
		root: s.Root, temporary: temporaryPath, final: filepath.Join(s.Root, name),
		ref: platformcontrolv1.SecretRef{Kind: "owner-file", Path: filepath.Join(s.Root, name)},
	}, nil
}

func (s *FileSecretMaterialStore) Reconcile(active *platformcontrolv1.SecretRef) error {
	if err := s.ensureRoot(); err != nil {
		return err
	}
	activePath := ""
	if active != nil && active.Kind == "owner-file" && filepath.Dir(active.Path) == s.Root && strings.HasPrefix(filepath.Base(active.Path), managedSecretPrefix) {
		activePath = active.Path
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(s.Root, name)
		if path == activePath || !strings.HasPrefix(name, managedSecretPrefix) && !strings.HasPrefix(name, ".platform-control-password-candidate-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("Bootstrap managed secret 目录包含非普通文件")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return syncDirectory(s.Root)
}

func (s *FileSecretMaterialStore) ensureRoot() error {
	if s == nil || !filepath.IsAbs(s.Root) || filepath.Clean(s.Root) != s.Root {
		return errors.New("Bootstrap managed secret 目录必须是规范绝对路径")
	}
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(s.Root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Bootstrap managed secret 目录必须是真实目录")
	}
	return os.Chmod(s.Root, 0o700)
}

type filePreparedSecret struct {
	mu         sync.Mutex
	root       string
	temporary  string
	final      string
	ref        platformcontrolv1.SecretRef
	committed  bool
	rolledBack bool
}

func (s *filePreparedSecret) Ref() platformcontrolv1.SecretRef { return s.ref }

func (s *filePreparedSecret) Source() platformcontrolport.SecretSource {
	s.mu.Lock()
	path := s.temporary
	if s.committed {
		path = s.final
	}
	s.mu.Unlock()
	source, _ := platformcontrolport.ResolveSecretSource(platformcontrolv1.SecretRef{Kind: "owner-file", Path: path}, "")
	return source
}

func (s *filePreparedSecret) Commit() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rolledBack {
		return errors.New("Bootstrap managed secret 已回滚")
	}
	if s.committed {
		return nil
	}
	if _, err := os.Lstat(s.final); err == nil {
		return errors.New("Bootstrap managed secret 目标已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(s.temporary, s.final); err != nil {
		return err
	}
	if err := syncDirectory(s.root); err != nil {
		_ = os.Remove(s.final)
		return err
	}
	s.committed = true
	return nil
}

func (s *filePreparedSecret) Rollback() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rolledBack {
		return nil
	}
	s.rolledBack = true
	return errors.Join(removeIfExists(s.temporary), removeIfExists(s.final), syncDirectory(s.root))
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
