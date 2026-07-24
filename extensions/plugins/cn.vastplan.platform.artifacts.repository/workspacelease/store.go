// Package workspacelease persists the short-lived workspace namespace of a
// local-test repository. Artifact bytes and trust evidence remain in the
// repository's ordinary immutable store; this package owns only visibility and
// lease lifecycle metadata.
package workspacelease

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const schemaVersion = "v1"

type Lease struct {
	Ref       pluginv1.ArtifactRef `json:"ref"`
	SHA256    string               `json:"sha256"`
	Token     string               `json:"token"`
	IssuedAt  time.Time            `json:"issuedAt"`
	ExpiresAt time.Time            `json:"expiresAt"`
}

type state struct {
	SchemaVersion string  `json:"schemaVersion"`
	Revision      uint64  `json:"revision"`
	Items         []Lease `json:"items"`
}

type Store struct {
	path string
	mu   sync.RWMutex
	data state
}

func (s *Store) CanGrant(ref pluginv1.ArtifactRef, sha256 string, maxArtifacts int, now time.Time) error {
	if maxArtifacts < 1 || ref.Channel != "workspace" || sha256 == "" {
		return errors.New("workspace lease 参数无效")
	}
	now = normalizedNow(now)
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, found := search(s.data.Items, ref)
	if found && s.data.Items[index].SHA256 != sha256 {
		return errors.New("workspace 不可变 ref 已绑定其他摘要")
	}
	if found && now.Before(s.data.Items[index].ExpiresAt) {
		return nil
	}
	active := 0
	for _, item := range s.data.Items {
		if now.Before(item.ExpiresAt) {
			active++
		}
	}
	if active >= maxArtifacts {
		return errors.New("workspace 活动候选已达到 Profile 容量上限")
	}
	return nil
}

func Open(repositoryRoot string) (*Store, error) {
	if !filepath.IsAbs(repositoryRoot) || filepath.Clean(repositoryRoot) != repositoryRoot {
		return nil, errors.New("workspace lease 仓库根目录必须是规范绝对路径")
	}
	store := &Store{
		path: filepath.Join(repositoryRoot, "catalog", "workspace-leases.json"),
		data: state{SchemaVersion: schemaVersion, Items: []Lease{}},
	}
	raw, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&store.data); err != nil {
		return nil, fmt.Errorf("解析 workspace lease 状态: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("workspace lease 状态只能包含一个 JSON 文档")
	}
	if store.data.SchemaVersion != schemaVersion || !sortedUnique(store.data.Items) {
		return nil, errors.New("workspace lease 状态版本或顺序无效")
	}
	return store, nil
}

// Grant is idempotent while an identical lease is active. An expired exact
// artifact receives a new opaque lease; immutable bytes are never overwritten.
func (s *Store) Grant(ref pluginv1.ArtifactRef, sha256 string, ttl time.Duration, maxArtifacts int, now time.Time) (Lease, uint64, error) {
	if ttl <= 0 || maxArtifacts < 1 || ref.Channel != "workspace" || sha256 == "" {
		return Lease{}, 0, errors.New("workspace lease 参数无效")
	}
	now = normalizedNow(now)
	s.mu.Lock()
	defer s.mu.Unlock()
	index, found := search(s.data.Items, ref)
	if found {
		prior := s.data.Items[index]
		if prior.SHA256 != sha256 {
			return Lease{}, s.data.Revision, errors.New("workspace 不可变 ref 已绑定其他摘要")
		}
		if now.Before(prior.ExpiresAt) {
			return prior, s.data.Revision, nil
		}
	}
	active := 0
	for i, item := range s.data.Items {
		if i != index || !found {
			if now.Before(item.ExpiresAt) {
				active++
			}
		}
	}
	if active >= maxArtifacts {
		return Lease{}, s.data.Revision, errors.New("workspace 活动候选已达到 Profile 容量上限")
	}
	tokenRaw := make([]byte, 32)
	if _, err := rand.Read(tokenRaw); err != nil {
		return Lease{}, s.data.Revision, fmt.Errorf("生成 workspace lease: %w", err)
	}
	lease := Lease{Ref: ref, SHA256: sha256, Token: base64.RawURLEncoding.EncodeToString(tokenRaw), IssuedAt: now, ExpiresAt: now.Add(ttl)}
	next := cloneState(s.data)
	next.Revision++
	if found {
		next.Items[index] = lease
	} else {
		next.Items = append(next.Items, Lease{})
		copy(next.Items[index+1:], next.Items[index:])
		next.Items[index] = lease
	}
	if err := s.save(next); err != nil {
		return Lease{}, s.data.Revision, err
	}
	s.data = next
	return lease, next.Revision, nil
}

func (s *Store) Active(ref pluginv1.ArtifactRef, now time.Time) (Lease, bool) {
	now = normalizedNow(now)
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, found := search(s.data.Items, ref)
	if !found || !now.Before(s.data.Items[index].ExpiresAt) {
		return Lease{}, false
	}
	return s.data.Items[index], true
}

func (s *Store) ActiveLeases(now time.Time) (uint64, []Lease) {
	now = normalizedNow(now)
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Lease, 0, len(s.data.Items))
	for _, item := range s.data.Items {
		if now.Before(item.ExpiresAt) {
			items = append(items, item)
		}
	}
	return s.data.Revision, items
}

// Expire removes only elapsed metadata that is not protected by a Test Release
// or another repository reference owner. Protected bytes and their audit
// metadata remain fail-closed until the owner publishes a replacement snapshot.
func (s *Store) Expire(now time.Time, protected func(pluginv1.ArtifactRef, string) bool) (uint64, int, error) {
	now = normalizedNow(now)
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneState(s.data)
	kept := next.Items[:0]
	expired := 0
	for _, item := range next.Items {
		if !now.Before(item.ExpiresAt) && (protected == nil || !protected(item.Ref, item.SHA256)) {
			expired++
			continue
		}
		kept = append(kept, item)
	}
	if expired == 0 {
		return s.data.Revision, 0, nil
	}
	next.Items = kept
	next.Revision++
	if err := s.save(next); err != nil {
		return s.data.Revision, 0, err
	}
	s.data = next
	return next.Revision, expired, nil
}

func (s *Store) save(next state) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, s.path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func search(items []Lease, ref pluginv1.ArtifactRef) (int, bool) {
	key := refKey(ref)
	index := sort.Search(len(items), func(i int) bool { return refKey(items[i].Ref) >= key })
	return index, index < len(items) && items[index].Ref == ref
}

func sortedUnique(items []Lease) bool {
	for index, item := range items {
		if item.Ref.Channel != "workspace" || item.SHA256 == "" || item.Token == "" || item.IssuedAt.IsZero() || !item.ExpiresAt.After(item.IssuedAt) {
			return false
		}
		if index > 0 && refKey(items[index-1].Ref) >= refKey(item.Ref) {
			return false
		}
	}
	return true
}

func refKey(ref pluginv1.ArtifactRef) string {
	return ref.PluginID + "\x00" + ref.Version + "\x00" + ref.Channel
}
func normalizedNow(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.UTC()
}
func cloneState(value state) state {
	value.Items = append([]Lease(nil), value.Items...)
	return value
}
