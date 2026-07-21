// Package garbagecollection persists crash-recoverable artifact retirement.
// Policy decisions stay in repositoryruntime; this package only advances the
// physical quarantining/sweeping state machine.
package garbagecollection

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const SchemaVersion = "v1"

var (
	refPluginPattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z0-9][a-z0-9-]*)+$`)
	refVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	refChannelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

const (
	StatusQuarantining = "quarantining"
	StatusQuarantined  = "quarantined"
	StatusSweeping     = "sweeping"
	StatusSwept        = "swept"
)

type Record struct {
	RetirementID  string               `json:"retirementId"`
	Ref           pluginv1.ArtifactRef `json:"ref"`
	SHA256        string               `json:"sha256"`
	Size          int64                `json:"size"`
	Lifecycle     string               `json:"lifecycle"`
	Status        string               `json:"status"`
	QuarantinedAt time.Time            `json:"quarantinedAt"`
	SweepAfter    time.Time            `json:"sweepAfter"`
	SweptAt       *time.Time           `json:"sweptAt,omitempty"`
}

type State struct {
	SchemaVersion string   `json:"schemaVersion"`
	Revision      uint64   `json:"revision"`
	Items         []Record `json:"items"`
}

type Storage interface {
	InspectRetirement(pluginv1.ArtifactRef, string, string) (string, error)
	QuarantineArtifact(pluginv1.ArtifactRef, string, string) error
	SweepArtifact(pluginv1.ArtifactRef, string, string) error
}

type Store struct {
	path string
	mu   sync.RWMutex
	data State
}

func Open(repositoryRoot string) (*Store, error) {
	if !filepath.IsAbs(repositoryRoot) || filepath.Clean(repositoryRoot) != repositoryRoot {
		return nil, errors.New("GC 仓库根目录必须是规范绝对路径")
	}
	store := &Store{path: filepath.Join(repositoryRoot, "catalog", "garbage-collection.json"), data: State{SchemaVersion: SchemaVersion, Items: []Record{}}}
	raw, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if err := decodeStrict(raw, &store.data); err != nil {
		return nil, fmt.Errorf("解析制品 GC 状态: %w", err)
	}
	if err := validateState(store.data); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) BeginQuarantine(record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.RetirementID == "" || record.Ref.PluginID == "" || record.Ref.Version == "" || record.Ref.Channel == "" || len(record.SHA256) != 64 || record.Size <= 0 || record.QuarantinedAt.IsZero() || !record.SweepAfter.After(record.QuarantinedAt) {
		return errors.New("GC 隔离记录无效")
	}
	key := recordKey(record.Ref, record.SHA256)
	index := sort.Search(len(s.data.Items), func(i int) bool { return recordKey(s.data.Items[i].Ref, s.data.Items[i].SHA256) >= key })
	if index < len(s.data.Items) && recordKey(s.data.Items[index].Ref, s.data.Items[index].SHA256) == key {
		return errors.New("制品已有 retirement 记录")
	}
	record.Status = StatusQuarantining
	next := cloneState(s.data)
	next.Revision++
	next.Items = append(next.Items, Record{})
	copy(next.Items[index+1:], next.Items[index:])
	next.Items[index] = record
	if err := s.save(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (s *Store) CompleteQuarantine(ref pluginv1.ArtifactRef, sha256 string) error {
	return s.transition(ref, sha256, StatusQuarantining, StatusQuarantined, nil)
}

func (s *Store) BeginSweep(ref pluginv1.ArtifactRef, sha256 string) error {
	return s.transition(ref, sha256, StatusQuarantined, StatusSweeping, nil)
}

func (s *Store) CompleteSweep(ref pluginv1.ArtifactRef, sha256 string, now time.Time) error {
	if now.IsZero() {
		return errors.New("GC sweep 完成时间无效")
	}
	value := now.UTC()
	return s.transition(ref, sha256, StatusSweeping, StatusSwept, &value)
}

func (s *Store) Recover(storage Storage, now time.Time) error {
	if storage == nil {
		return errors.New("GC 恢复缺少物理仓库")
	}
	for _, record := range s.List().Items {
		switch record.Status {
		case StatusQuarantining:
			if err := storage.QuarantineArtifact(record.Ref, record.SHA256, record.RetirementID); err != nil {
				return fmt.Errorf("恢复制品隔离 %s: %w", recordKey(record.Ref, record.SHA256), err)
			}
			if err := s.CompleteQuarantine(record.Ref, record.SHA256); err != nil {
				return err
			}
		case StatusSweeping:
			if err := storage.SweepArtifact(record.Ref, record.SHA256, record.RetirementID); err != nil {
				return fmt.Errorf("恢复制品清扫 %s: %w", recordKey(record.Ref, record.SHA256), err)
			}
			if err := s.CompleteSweep(record.Ref, record.SHA256, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) IsRetired(ref pluginv1.ArtifactRef, sha256 string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.findLocked(ref, sha256)
	return ok
}

// AllowsMissing implements catalog.MissingArtifactRegistry.
func (s *Store) AllowsMissing(ref pluginv1.ArtifactRef, sha256 string) bool {
	return s.IsRetired(ref, sha256)
}

func (s *Store) List() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneState(s.data)
}

func (s *Store) Due(now time.Time) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]Record, 0)
	for _, record := range s.data.Items {
		if record.Status == StatusQuarantined && !now.UTC().Before(record.SweepAfter) {
			values = append(values, record)
		}
	}
	return values
}

func (s *Store) transition(ref pluginv1.ArtifactRef, sha256, from, to string, sweptAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, ok := s.findLocked(ref, sha256)
	if !ok || s.data.Items[index].Status != from {
		return fmt.Errorf("GC 状态不能从 %s 转为 %s", from, to)
	}
	next := cloneState(s.data)
	next.Revision++
	next.Items[index].Status = to
	next.Items[index].SweptAt = sweptAt
	if err := s.save(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

func (s *Store) findLocked(ref pluginv1.ArtifactRef, sha256 string) (int, bool) {
	key := recordKey(ref, sha256)
	index := sort.Search(len(s.data.Items), func(i int) bool { return recordKey(s.data.Items[i].Ref, s.data.Items[i].SHA256) >= key })
	return index, index < len(s.data.Items) && recordKey(s.data.Items[index].Ref, s.data.Items[index].SHA256) == key
}

func (s *Store) save(value State) error {
	if err := validateState(value); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".garbage-collection-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	_, writeErr := file.Write(append(raw, '\n'))
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}

func validateState(value State) error {
	if value.SchemaVersion != SchemaVersion {
		return errors.New("制品 GC 状态版本不受支持")
	}
	previous := ""
	for _, record := range value.Items {
		key := recordKey(record.Ref, record.SHA256)
		if key <= previous || !validDigest(record.RetirementID) || !validDigest(record.SHA256) || !refPluginPattern.MatchString(record.Ref.PluginID) || !refVersionPattern.MatchString(record.Ref.Version) || !refChannelPattern.MatchString(record.Ref.Channel) || record.Size <= 0 || (record.Lifecycle != "yanked" && record.Lifecycle != "revoked") || record.QuarantinedAt.IsZero() || !record.SweepAfter.After(record.QuarantinedAt) || !validStatus(record.Status) || (record.Status == StatusSwept) != (record.SweptAt != nil) {
			return errors.New("持久化制品 GC 记录无效")
		}
		previous = key
	}
	return nil
}

func validDigest(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32
}

func validStatus(value string) bool {
	return value == StatusQuarantining || value == StatusQuarantined || value == StatusSweeping || value == StatusSwept
}

func recordKey(ref pluginv1.ArtifactRef, sha256 string) string {
	return ref.PluginID + "@" + ref.Version + "/" + ref.Channel + "\x00" + sha256
}

func cloneState(value State) State {
	copy := value
	copy.Items = append([]Record(nil), value.Items...)
	for index := range copy.Items {
		if value.Items[index].SweptAt != nil {
			timestamp := *value.Items[index].SweptAt
			copy.Items[index].SweptAt = &timestamp
		}
	}
	return copy
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("JSON 只能包含一个值")
	}
	return nil
}
