package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactstorage"
)

const migrationRecordVersion = "v1"

type migrationRecord struct {
	SchemaVersion string `json:"schemaVersion"`
	MigrationID   string `json:"migrationId"`
	SourceVolume  string `json:"sourceVolumeId"`
	TargetVolume  string `json:"targetVolumeId"`
	SourceHandle  string `json:"sourceHandle"`
	TargetHandle  string `json:"targetHandle"`
	Phase         string `json:"phase"`
	UpdatedAt     string `json:"updatedAt"`
}

type inventoryItem struct {
	Path   string
	Size   int64
	SHA256 string
}

func (s *Service) Migrate(ctx context.Context, request artifactstorage.VolumeMigrationRequest) (artifactstorage.VolumeMigrationResult, error) {
	if err := validateMigrationRequest(request); err != nil {
		return artifactstorage.VolumeMigrationResult{}, err
	}
	s.migrationMu.Lock()
	defer s.migrationMu.Unlock()

	source, err := s.Describe(request.SourceVolumeID)
	if err != nil {
		return artifactstorage.VolumeMigrationResult{}, fmt.Errorf("读取源 volume: %w", err)
	}
	record, exists, err := s.readMigration(request.MigrationID)
	if err != nil {
		return artifactstorage.VolumeMigrationResult{}, err
	}
	if exists && (record.SourceVolume != request.SourceVolumeID || record.TargetVolume != request.TargetVolumeID || record.SourceHandle != source.Handle) {
		return artifactstorage.VolumeMigrationResult{}, errors.New("同一 migrationId 已绑定其他 volume")
	}

	if request.Phase == artifactstorage.MigrationPrepare {
		target, err := s.prepareMigrationTarget(request, exists)
		if err != nil {
			return artifactstorage.VolumeMigrationResult{}, err
		}
		if !exists {
			record = migrationRecord{SchemaVersion: migrationRecordVersion, MigrationID: request.MigrationID, SourceVolume: request.SourceVolumeID, TargetVolume: request.TargetVolumeID, SourceHandle: source.Handle, TargetHandle: target.Handle}
		}
		record.Phase, record.UpdatedAt = artifactstorage.MigrationPrepare, time.Now().UTC().Format(time.RFC3339Nano)
		if err := s.writeMigration(record); err != nil {
			return artifactstorage.VolumeMigrationResult{}, err
		}
		return artifactstorage.VolumeMigrationResult{MigrationID: request.MigrationID, Phase: request.Phase, Source: source, Target: target, Ready: true}, nil
	}
	if !exists {
		return artifactstorage.VolumeMigrationResult{}, errors.New("迁移尚未 prepare")
	}
	target, err := s.Describe(request.TargetVolumeID)
	if err != nil || target.Handle != record.TargetHandle {
		return artifactstorage.VolumeMigrationResult{}, errors.New("迁移目标 volume 不可用或身份已变更")
	}

	var items []inventoryItem
	if request.Phase == artifactstorage.MigrationSync {
		items, err = syncDirectory(ctx, source.MountPath, target.MountPath)
	} else {
		items, err = compareDirectories(ctx, source.MountPath, target.MountPath)
	}
	if err != nil {
		return artifactstorage.VolumeMigrationResult{}, err
	}
	record.Phase, record.UpdatedAt = request.Phase, time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.writeMigration(record); err != nil {
		return artifactstorage.VolumeMigrationResult{}, err
	}
	files, bytes, digest := summarizeInventory(items)
	return artifactstorage.VolumeMigrationResult{MigrationID: request.MigrationID, Phase: request.Phase, Source: source, Target: target, Files: files, Bytes: bytes, Digest: digest, Ready: true}, nil
}

func (s *Service) Release(request artifactstorage.VolumeReleaseRequest) (artifactstorage.VolumeReleaseResult, error) {
	if err := artifactstorage.ValidateMigrationID(request.MigrationID); err != nil {
		return artifactstorage.VolumeReleaseResult{}, err
	}
	if err := artifactstorage.ValidateVolumeID(request.VolumeID); err != nil {
		return artifactstorage.VolumeReleaseResult{}, err
	}
	s.migrationMu.Lock()
	defer s.migrationMu.Unlock()
	record, exists, err := s.readMigration(request.MigrationID)
	if err != nil || !exists {
		return artifactstorage.VolumeReleaseResult{}, errors.New("迁移记录不存在")
	}
	if request.VolumeID != record.SourceVolume || request.ExpectedHandle == "" || request.ExpectedHandle != record.SourceHandle {
		return artifactstorage.VolumeReleaseResult{}, errors.New("release 只能回收迁移绑定的原 source volume")
	}
	source := s.volumePath(request.VolumeID)
	quarantine := filepath.Join(s.quarantineRoot(), request.MigrationID+"--"+request.VolumeID)
	if _, err := os.Lstat(quarantine); err == nil {
		if _, sourceErr := os.Lstat(source); errors.Is(sourceErr, os.ErrNotExist) {
			record.Phase, record.UpdatedAt = "released", time.Now().UTC().Format(time.RFC3339Nano)
			if err := s.writeMigration(record); err != nil {
				return artifactstorage.VolumeReleaseResult{}, err
			}
			return artifactstorage.VolumeReleaseResult{MigrationID: request.MigrationID, VolumeID: request.VolumeID, Released: true}, nil
		}
		return artifactstorage.VolumeReleaseResult{}, errors.New("回收隔离目录已存在但源 volume 仍存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return artifactstorage.VolumeReleaseResult{}, err
	}
	if err := os.Rename(source, quarantine); err != nil {
		return artifactstorage.VolumeReleaseResult{}, fmt.Errorf("原子隔离 source volume: %w", err)
	}
	record.Phase, record.UpdatedAt = "released", time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.writeMigration(record); err != nil {
		return artifactstorage.VolumeReleaseResult{}, err
	}
	return artifactstorage.VolumeReleaseResult{MigrationID: request.MigrationID, VolumeID: request.VolumeID, Released: true}, nil
}

func validateMigrationRequest(request artifactstorage.VolumeMigrationRequest) error {
	if err := artifactstorage.ValidateMigrationID(request.MigrationID); err != nil {
		return err
	}
	if err := artifactstorage.ValidateVolumeID(request.SourceVolumeID); err != nil {
		return err
	}
	if err := artifactstorage.ValidateVolumeID(request.TargetVolumeID); err != nil {
		return err
	}
	if request.SourceVolumeID == request.TargetVolumeID {
		return errors.New("迁移源与目标 volume 不能相同")
	}
	return artifactstorage.ValidateMigrationPhase(request.Phase)
}

func (s *Service) prepareMigrationTarget(request artifactstorage.VolumeMigrationRequest, existing bool) (artifactstorage.Volume, error) {
	path := s.volumePath(request.TargetVolumeID)
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return artifactstorage.Volume{}, err
		}
	} else if err != nil {
		return artifactstorage.Volume{}, err
	} else if !existing {
		entries, readErr := os.ReadDir(path)
		if readErr != nil {
			return artifactstorage.Volume{}, readErr
		}
		if len(entries) != 0 {
			return artifactstorage.Volume{}, errors.New("新迁移目标 volume 必须为空")
		}
	}
	return s.Describe(request.TargetVolumeID)
}

func syncDirectory(ctx context.Context, source, target string) ([]inventoryItem, error) {
	// Reject hostile or corrupted target types before creating any child path;
	// otherwise a planted directory symlink could redirect a copy outside the
	// provisioned volume.
	if _, _, err := inventory(ctx, target); err != nil {
		return nil, err
	}
	items, directories, err := inventory(ctx, source)
	if err != nil {
		return nil, err
	}
	for _, relative := range directories {
		if err := os.MkdirAll(filepath.Join(target, filepath.FromSlash(relative)), 0o700); err != nil {
			return nil, err
		}
	}
	allowed := make(map[string]struct{}, len(items)+len(directories))
	for _, directory := range directories {
		allowed[filepath.ToSlash(directory)+"/"] = struct{}{}
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		allowed[item.Path] = struct{}{}
		if err := copyVerifiedFile(ctx, filepath.Join(source, filepath.FromSlash(item.Path)), filepath.Join(target, filepath.FromSlash(item.Path)), item); err != nil {
			return nil, err
		}
	}
	if err := removeTargetExtras(target, allowed); err != nil {
		return nil, err
	}
	targetItems, _, err := inventory(ctx, target)
	if err != nil {
		return nil, err
	}
	if err := compareInventory(items, targetItems); err != nil {
		return nil, err
	}
	return items, nil
}

func compareDirectories(ctx context.Context, source, target string) ([]inventoryItem, error) {
	sourceItems, _, err := inventory(ctx, source)
	if err != nil {
		return nil, err
	}
	targetItems, _, err := inventory(ctx, target)
	if err != nil {
		return nil, err
	}
	if err := compareInventory(sourceItems, targetItems); err != nil {
		return nil, err
	}
	return sourceItems, nil
}

func compareInventory(sourceItems, targetItems []inventoryItem) error {
	if len(sourceItems) != len(targetItems) {
		return errors.New("迁移源与目标文件数不一致")
	}
	for index := range sourceItems {
		if sourceItems[index] != targetItems[index] {
			return fmt.Errorf("迁移源与目标不一致: %s", sourceItems[index].Path)
		}
	}
	return nil
}

func inventory(ctx context.Context, root string) ([]inventoryItem, []string, error) {
	items := []inventoryItem{}
	directories := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("制品 volume 不允许符号链接: %s", relative)
		}
		if entry.IsDir() {
			directories = append(directories, relative)
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("制品 volume 只允许普通文件: %s", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		digest, err := pathSHA256(path)
		if err != nil {
			return err
		}
		items = append(items, inventoryItem{Path: relative, Size: info.Size(), SHA256: digest})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	sort.Strings(directories)
	return items, directories, nil
}

func copyVerifiedFile(ctx context.Context, source, target string, expected inventoryItem) error {
	if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() && info.Size() == expected.Size {
		if digest, hashErr := pathSHA256(target); hashErr == nil && digest == expected.SHA256 {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(target), ".migration-*")
	if err != nil {
		return err
	}
	temporary := out.Name()
	committed := false
	defer func() {
		_ = out.Close()
		if !committed {
			_ = os.Remove(temporary)
		}
	}()
	if err := out.Chmod(0o600); err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, hash), contextReader{ctx: ctx, reader: in})
	if err := errors.Join(copyErr, out.Sync(), out.Close()); err != nil {
		return err
	}
	if written != expected.Size || hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		return fmt.Errorf("复制期间源文件发生变化: %s", expected.Path)
	}
	if err := os.Rename(temporary, target); err != nil {
		return err
	}
	committed = true
	return nil
}

func removeTargetExtras(root string, allowed map[string]struct{}) error {
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root {
			paths = append(paths, path)
		}
		return nil
	}); err != nil {
		return err
	}
	sort.Slice(paths, func(i, j int) bool { return len(paths[i]) > len(paths[j]) })
	for _, path := range paths {
		relative, _ := filepath.Rel(root, path)
		key := filepath.ToSlash(relative)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			key += "/"
		}
		if _, ok := allowed[key]; ok {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func summarizeInventory(items []inventoryItem) (int64, int64, string) {
	hash := sha256.New()
	var bytes int64
	for _, item := range items {
		bytes += item.Size
		_, _ = fmt.Fprintf(hash, "%s\x00%d\x00%s\n", item.Path, item.Size, item.SHA256)
	}
	return int64(len(items)), bytes, hex.EncodeToString(hash.Sum(nil))
}

func pathSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Service) migrationRoot() string  { return filepath.Join(s.controlRoot(), "migrations") }
func (s *Service) quarantineRoot() string { return filepath.Join(s.controlRoot(), "quarantine") }
func (s *Service) migrationPath(id string) string {
	return filepath.Join(s.migrationRoot(), id+".json")
}

func (s *Service) readMigration(id string) (migrationRecord, bool, error) {
	raw, err := os.ReadFile(s.migrationPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return migrationRecord{}, false, nil
	}
	if err != nil {
		return migrationRecord{}, false, err
	}
	var record migrationRecord
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil || record.SchemaVersion != migrationRecordVersion || record.MigrationID != id {
		return migrationRecord{}, false, errors.New("制品存储迁移记录无效")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return migrationRecord{}, false, errors.New("制品存储迁移记录包含多余数据")
	}
	return record, true, nil
}

func (s *Service) writeMigration(record migrationRecord) error {
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(s.migrationRoot(), ".migration-state-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		return err
	}
	if err := errors.Join(temporary.Sync(), temporary.Close()); err != nil {
		return err
	}
	if err := os.Rename(name, s.migrationPath(record.MigrationID)); err != nil {
		return err
	}
	committed = true
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}
