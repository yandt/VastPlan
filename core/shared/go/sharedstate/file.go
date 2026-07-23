package sharedstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const fileFormatVersion = 1

// FileStore is the single-process development provider. It is intentionally
// not advertised as a clustered provider; production active-active services
// must use a CAS-capable external provider such as NATSStore.
type FileStore struct {
	mu       sync.Mutex
	path     string
	revision uint64
	entries  map[string]fileEntry
}

type fileDocument struct {
	FormatVersion int                  `json:"formatVersion"`
	Revision      uint64               `json:"revision"`
	Entries       map[string]fileEntry `json:"entries"`
}

type fileEntry struct {
	Value     []byte    `json:"value"`
	Revision  uint64    `json:"revision"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func OpenFileStore(path string) (*FileStore, error) {
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return nil, errors.New("shared state file 必须是绝对路径")
	}
	store := &FileStore{path: filepath.Clean(path), entries: map[string]fileEntry{}}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *FileStore) Get(_ context.Context, scope Scope, key string) (Entry, error) {
	physical, err := physicalKey(scope, key)
	if err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.entries[physical]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return fileStateEntry(key, value), nil
}

func (s *FileStore) Create(_ context.Context, scope Scope, key string, value []byte) (Entry, error) {
	return s.write(scope, key, value, 0, true)
}

func (s *FileStore) Update(_ context.Context, scope Scope, key string, value []byte, expected uint64) (Entry, error) {
	if expected == 0 {
		return Entry{}, ErrInvalid
	}
	return s.write(scope, key, value, expected, false)
}

func (s *FileStore) Delete(_ context.Context, scope Scope, key string, expected uint64) error {
	physical, err := physicalKey(scope, key)
	if err != nil || expected == 0 {
		if err != nil {
			return err
		}
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.entries[physical]
	if !ok {
		return ErrNotFound
	}
	if current.Revision != expected {
		return ErrConflict
	}
	previous := cloneFileEntries(s.entries)
	delete(s.entries, physical)
	s.revision++
	if err := s.save(); err != nil {
		s.entries = previous
		s.revision--
		return err
	}
	return nil
}

func (s *FileStore) List(_ context.Context, scope Scope, prefix string, limit int, cursor string) (Page, error) {
	if err := scope.Validate(); err != nil {
		return Page{}, err
	}
	if err := ValidateList(prefix, limit, cursor); err != nil {
		return Page{}, err
	}
	physicalPrefix := scopePrefix(scope)
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0)
	for physical := range s.entries {
		if !strings.HasPrefix(physical, physicalPrefix) {
			continue
		}
		logical, err := decode(strings.TrimPrefix(physical, physicalPrefix))
		if err == nil && strings.HasPrefix(logical, prefix) && logical > cursor {
			keys = append(keys, logical)
		}
	}
	sort.Strings(keys)
	page := Page{Items: []Entry{}}
	for _, key := range keys {
		if len(page.Items) == limit {
			page.NextCursor = page.Items[len(page.Items)-1].Key
			break
		}
		page.Items = append(page.Items, fileStateEntry(key, s.entries[physicalPrefix+encode(key)]))
	}
	return page, nil
}

func (s *FileStore) write(scope Scope, key string, value []byte, expected uint64, create bool) (Entry, error) {
	physical, err := physicalKey(scope, key)
	if err != nil {
		return Entry{}, err
	}
	if err := ValidateValue(value); err != nil {
		return Entry{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.entries[physical]
	if create && exists {
		return Entry{}, ErrConflict
	}
	if !create && (!exists || current.Revision != expected) {
		return Entry{}, ErrConflict
	}
	previous := cloneFileEntries(s.entries)
	s.revision++
	next := fileEntry{Value: append([]byte(nil), value...), Revision: s.revision, UpdatedAt: time.Now().UTC()}
	s.entries[physical] = next
	if err := s.save(); err != nil {
		s.entries = previous
		s.revision--
		return Entry{}, err
	}
	return fileStateEntry(key, next), nil
}

func (s *FileStore) load() error {
	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("shared state file 必须是权限 0600 的普通文件")
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var document fileDocument
	if err := json.Unmarshal(raw, &document); err != nil || document.FormatVersion != fileFormatVersion || document.Entries == nil {
		return errors.New("shared state file 格式无效")
	}
	for physical, entry := range document.Entries {
		if physical == "" || entry.Revision == 0 || entry.Revision > document.Revision || entry.UpdatedAt.IsZero() || ValidateValue(entry.Value) != nil {
			return errors.New("shared state file 条目无效")
		}
	}
	s.revision, s.entries = document.Revision, cloneFileEntries(document.Entries)
	return nil
}

func (s *FileStore) save() error {
	parent := filepath.Dir(s.path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(fileDocument{FormatVersion: fileFormatVersion, Revision: s.revision, Entries: s.entries})
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".shared-state-*")
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
	if err := os.Rename(name, s.path); err != nil {
		return err
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync shared state directory: %w", err)
	}
	return nil
}

func fileStateEntry(key string, value fileEntry) Entry {
	return Entry{Key: key, Value: append([]byte(nil), value.Value...), Revision: value.Revision, UpdatedAt: value.UpdatedAt}
}

func cloneFileEntries(source map[string]fileEntry) map[string]fileEntry {
	out := make(map[string]fileEntry, len(source))
	for key, value := range source {
		value.Value = append([]byte(nil), value.Value...)
		out[key] = value
	}
	return out
}
