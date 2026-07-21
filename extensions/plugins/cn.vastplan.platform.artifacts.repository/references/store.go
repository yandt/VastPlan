// Package references persists complete artifact-reference snapshots published
// by trusted consumers. Expiry makes GC unhealthy; it never removes protection.
package references

import (
	"bytes"
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
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
)

const stateSchemaVersion = "v1"

type Snapshot struct {
	TenantID    string                             `json:"tenantId"`
	PublisherID string                             `json:"publisherId"`
	Value       pluginv1.ArtifactReferenceSnapshot `json:"value"`
	ReportedAt  time.Time                          `json:"reportedAt"`
	ExpiresAt   *time.Time                         `json:"expiresAt,omitempty"`
}

type RequiredOwner struct {
	TenantID  string `json:"tenantId"`
	OwnerKind string `json:"ownerKind"`
	OwnerID   string `json:"ownerId"`
}

type Health struct {
	Ready    bool            `json:"ready"`
	Revision uint64          `json:"revision"`
	Missing  []RequiredOwner `json:"missing,omitempty"`
	Expired  []RequiredOwner `json:"expired,omitempty"`
}

type state struct {
	SchemaVersion string     `json:"schemaVersion"`
	Revision      uint64     `json:"revision"`
	Snapshots     []Snapshot `json:"snapshots"`
}

type Store struct {
	path string
	mu   sync.RWMutex
	data state
}

func Open(repositoryRoot string) (*Store, error) {
	if !filepath.IsAbs(repositoryRoot) || filepath.Clean(repositoryRoot) != repositoryRoot {
		return nil, errors.New("引用仓库根目录必须是规范绝对路径")
	}
	path := filepath.Join(repositoryRoot, "catalog", "references.json")
	store := &Store{path: path, data: state{SchemaVersion: stateSchemaVersion, Snapshots: []Snapshot{}}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	if err := decodeStrict(raw, &store.data); err != nil {
		return nil, fmt.Errorf("解析制品引用状态: %w", err)
	}
	if store.data.SchemaVersion != stateSchemaVersion {
		return nil, errors.New("制品引用状态版本不受支持")
	}
	if err := validateSnapshots(store.data.Snapshots); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Put(tenantID, publisherID string, value pluginv1.ArtifactReferenceSnapshot, now time.Time) (Snapshot, uint64, error) {
	if publisherID == "" || len(publisherID) > 256 {
		return Snapshot{}, 0, errors.New("引用发布者身份无效")
	}
	if err := artifactreference.Validate(value); err != nil {
		return Snapshot{}, 0, err
	}
	key, err := artifactreference.SnapshotKey(tenantID, value)
	if err != nil {
		return Snapshot{}, 0, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	index := sort.Search(len(s.data.Snapshots), func(i int) bool { return snapshotKey(s.data.Snapshots[i]) >= key })
	if index < len(s.data.Snapshots) && snapshotKey(s.data.Snapshots[index]) == key {
		prior := s.data.Snapshots[index]
		if prior.PublisherID != publisherID {
			return Snapshot{}, s.data.Revision, errors.New("引用 owner 已绑定其他可信发布者")
		}
		if value.Generation < prior.Value.Generation || value.Generation == prior.Value.Generation && value.Digest != prior.Value.Digest {
			return Snapshot{}, s.data.Revision, errors.New("引用快照 generation 回退或同代内容漂移")
		}
		updated := storedSnapshot(tenantID, publisherID, value, now)
		next := cloneState(s.data)
		next.Revision++
		next.Snapshots[index] = updated
		if err := s.save(next); err != nil {
			return Snapshot{}, s.data.Revision, err
		}
		s.data = next
		return cloneSnapshot(updated), next.Revision, nil
	}
	created := storedSnapshot(tenantID, publisherID, value, now)
	next := cloneState(s.data)
	next.Revision++
	next.Snapshots = append(next.Snapshots, Snapshot{})
	copy(next.Snapshots[index+1:], next.Snapshots[index:])
	next.Snapshots[index] = created
	if err := s.save(next); err != nil {
		return Snapshot{}, s.data.Revision, err
	}
	s.data = next
	return cloneSnapshot(created), next.Revision, nil
}

func (s *Store) List() (uint64, []Snapshot) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]Snapshot, len(s.data.Snapshots))
	for index := range s.data.Snapshots {
		values[index] = cloneSnapshot(s.data.Snapshots[index])
	}
	return s.data.Revision, values
}

func (s *Store) Protected() map[pluginv1.ArtifactRef]map[string]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	protected := map[pluginv1.ArtifactRef]map[string]struct{}{}
	for _, snapshot := range s.data.Snapshots {
		owner := snapshotKey(snapshot)
		for _, reference := range snapshot.Value.References {
			owners := protected[reference.Ref]
			if owners == nil {
				owners = map[string]struct{}{}
				protected[reference.Ref] = owners
			}
			owners[owner] = struct{}{}
		}
	}
	return protected
}

func (s *Store) Health(required []RequiredOwner, now time.Time) Health {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	byKey := make(map[string]Snapshot, len(s.data.Snapshots))
	for _, snapshot := range s.data.Snapshots {
		byKey[snapshotKey(snapshot)] = snapshot
	}
	health := Health{Ready: true, Revision: s.data.Revision}
	for _, owner := range required {
		key := owner.TenantID + "\x00" + owner.OwnerKind + "\x00" + owner.OwnerID
		snapshot, ok := byKey[key]
		if !ok {
			health.Ready = false
			health.Missing = append(health.Missing, owner)
		} else if snapshot.ExpiresAt != nil && !now.UTC().Before(*snapshot.ExpiresAt) {
			health.Ready = false
			health.Expired = append(health.Expired, owner)
		}
	}
	return health
}

func storedSnapshot(tenantID, publisherID string, value pluginv1.ArtifactReferenceSnapshot, now time.Time) Snapshot {
	result := Snapshot{TenantID: tenantID, PublisherID: publisherID, Value: cloneValue(value), ReportedAt: now}
	if value.TTLSeconds > 0 {
		expires := now.Add(time.Duration(value.TTLSeconds) * time.Second)
		result.ExpiresAt = &expires
	}
	return result
}

func validateSnapshots(values []Snapshot) error {
	previous := ""
	for _, snapshot := range values {
		key, err := artifactreference.SnapshotKey(snapshot.TenantID, snapshot.Value)
		if err != nil || key <= previous || snapshot.PublisherID == "" || snapshot.ReportedAt.IsZero() || artifactreference.Validate(snapshot.Value) != nil {
			return errors.New("持久化制品引用快照无效")
		}
		previous = key
	}
	return nil
}

func snapshotKey(snapshot Snapshot) string {
	return snapshot.TenantID + "\x00" + snapshot.Value.OwnerKind + "\x00" + snapshot.Value.OwnerID
}

func cloneState(value state) state {
	copy := value
	copy.Snapshots = make([]Snapshot, len(value.Snapshots))
	for index := range value.Snapshots {
		copy.Snapshots[index] = cloneSnapshot(value.Snapshots[index])
	}
	return copy
}

func cloneSnapshot(value Snapshot) Snapshot {
	copy := value
	copy.Value = cloneValue(value.Value)
	if value.ExpiresAt != nil {
		expires := *value.ExpiresAt
		copy.ExpiresAt = &expires
	}
	return copy
}

func cloneValue(value pluginv1.ArtifactReferenceSnapshot) pluginv1.ArtifactReferenceSnapshot {
	copy := value
	copy.References = append([]pluginv1.ArtifactReference(nil), value.References...)
	return copy
}

func (s *Store) save(value state) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".references-*")
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
