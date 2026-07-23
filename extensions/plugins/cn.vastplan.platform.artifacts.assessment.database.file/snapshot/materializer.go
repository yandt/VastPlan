package snapshot

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	provider "cdsoft.com.cn/VastPlan/extensions/sdk/go/artifactassessmentprovider"
)

const (
	maxMetadataBytes int64 = 4 << 20
	maxDatabaseBytes int64 = 2 << 30
)

type Status struct {
	Ready            bool   `json:"ready"`
	DatabaseRevision string `json:"databaseRevision"`
	Files            int    `json:"files"`
	Bytes            int64  `json:"bytes"`
}

type Materializer struct {
	config Config
	mu     sync.Mutex
	status Status
}

func New(config Config) (*Materializer, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Materializer{config: config, status: Status{DatabaseRevision: config.DatabaseRevision}}, nil
}

func (m *Materializer) Materialize() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ensurePrivateDirectory(m.config.SnapshotRoot); err != nil {
		return m.fail(err)
	}
	snapshotsRoot := filepath.Join(m.config.SnapshotRoot, "snapshots")
	if err := ensurePrivateDirectory(snapshotsRoot); err != nil {
		return m.fail(err)
	}
	final := m.config.SnapshotDirectory()
	if _, err := os.Lstat(final); err == nil {
		return m.verify(final)
	} else if !errors.Is(err, os.ErrNotExist) {
		return m.fail(err)
	}
	if err := requirePrivateDirectory(m.config.SourceDirectory); err != nil {
		return m.fail(fmt.Errorf("校验 Trivy database staging: %w", err))
	}
	if err := requirePrivateDirectory(filepath.Join(m.config.SourceDirectory, "db")); err != nil {
		return m.fail(fmt.Errorf("校验 Trivy database staging db: %w", err))
	}
	candidate, err := os.MkdirTemp(snapshotsRoot, ".candidate-")
	if err != nil {
		return m.fail(err)
	}
	defer func() { _ = os.RemoveAll(candidate) }()
	if err := os.Chmod(candidate, 0o700); err != nil {
		return m.fail(err)
	}
	databaseDirectory := filepath.Join(candidate, "db")
	if err := os.Mkdir(databaseDirectory, 0o700); err != nil {
		return m.fail(err)
	}
	for _, file := range []struct {
		name string
		max  int64
	}{{"metadata.json", maxMetadataBytes}, {"trivy.db", maxDatabaseBytes}} {
		if err := copyPrivateRegular(filepath.Join(m.config.SourceDirectory, "db", file.name), filepath.Join(databaseDirectory, file.name), file.max); err != nil {
			return m.fail(err)
		}
	}
	for _, directory := range []string{databaseDirectory, candidate} {
		if err := syncDirectory(directory); err != nil {
			return m.fail(err)
		}
	}
	revision, err := provider.TrivyDatabaseRevision(candidate)
	if err != nil || revision != m.config.DatabaseRevision {
		return m.fail(errors.New("Trivy database candidate 与配置 revision 不一致"))
	}
	if err := os.Rename(candidate, final); err != nil {
		if _, statErr := os.Lstat(final); statErr == nil {
			return m.verify(final)
		}
		return m.fail(fmt.Errorf("原子发布 Trivy database snapshot: %w", err))
	}
	if err := syncDirectory(snapshotsRoot); err != nil {
		return m.fail(err)
	}
	return m.verify(final)
}

func (m *Materializer) Current() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Materializer) verify(root string) (Status, error) {
	if err := requirePrivateDirectory(root); err != nil {
		return m.fail(err)
	}
	if err := requirePrivateDirectory(filepath.Join(root, "db")); err != nil {
		return m.fail(err)
	}
	var total int64
	for _, name := range []string{"metadata.json", "trivy.db"} {
		info, err := os.Lstat(filepath.Join(root, "db", name))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 {
			return m.fail(errors.New("Trivy database snapshot 文件无效"))
		}
		total += info.Size()
	}
	revision, err := provider.TrivyDatabaseRevision(root)
	if err != nil || revision != m.config.DatabaseRevision {
		return m.fail(errors.New("已发布 Trivy database snapshot 摘要无效"))
	}
	m.status = Status{Ready: true, DatabaseRevision: revision, Files: 2, Bytes: total}
	return m.status, nil
}

func (m *Materializer) fail(err error) (Status, error) {
	m.status = Status{DatabaseRevision: m.config.DatabaseRevision}
	return m.status, err
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return requirePrivateDirectory(path)
}

func requirePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("Trivy database 目录必须仅属主可访问且非符号链接")
	}
	return nil
}

func copyPrivateRegular(source, destination string, limit int64) error {
	before, err := os.Lstat(source)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > limit {
		return errors.New("Trivy database staging 文件缺失、不是普通文件或大小超限")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	after, err := input.Stat()
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() {
		return errors.New("Trivy database staging 文件打开期间身份变化")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(input, limit+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || written != before.Size() || written > limit || syncErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		return errors.New("复制 Trivy database staging 文件失败或大小漂移")
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
