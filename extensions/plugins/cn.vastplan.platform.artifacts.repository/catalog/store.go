// Package catalog implements the managed repository's derived artifact catalog
// and append-only publish journal. Trust and immutable storage remain owned by
// pluginservice; this package only indexes artifacts that the signed repository
// has already verified.
package catalog

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

const schemaVersion = "v1"

type VerifiedRepository interface {
	ListRefs() ([]pluginservice.Ref, error)
	ReadMetadataWithAttestation(pluginservice.Ref) (pluginservice.Artifact, []byte, error)
}

type Event struct {
	SchemaVersion string               `json:"schemaVersion"`
	Revision      uint64               `json:"revision"`
	Type          string               `json:"type"`
	Ref           pluginv1.ArtifactRef `json:"ref"`
	SHA256        string               `json:"sha256"`
	Size          int64                `json:"size"`
	Publisher     string               `json:"publisher"`
	KeyID         string               `json:"keyId"`
	SignedAt      time.Time            `json:"signedAt"`
	OccurredAt    time.Time            `json:"occurredAt"`
	Recovered     bool                 `json:"recovered,omitempty"`
}

type Entry struct {
	Ref                  pluginv1.ArtifactRef               `json:"ref"`
	SHA256               string                             `json:"sha256"`
	Size                 int64                              `json:"size"`
	Publisher            string                             `json:"publisher"`
	KeyID                string                             `json:"keyId"`
	SignedAt             time.Time                          `json:"signedAt"`
	PublishedAt          time.Time                          `json:"publishedAt"`
	RepositoryRevision   uint64                             `json:"repositoryRevision"`
	Name                 string                             `json:"name"`
	Description          string                             `json:"description"`
	Namespace            string                             `json:"namespace"`
	License              string                             `json:"license,omitempty"`
	Engines              map[string]string                  `json:"engines"`
	Dependencies         map[string]string                  `json:"dependencies,omitempty"`
	Targets              []string                           `json:"targets"`
	Platforms            []string                           `json:"platforms,omitempty"`
	RuntimeRequires      []pluginv1.RuntimeRequirement      `json:"runtimeRequires,omitempty"`
	RuntimeProvides      []pluginv1.RuntimeCapabilityPolicy `json:"runtimeProvides,omitempty"`
	ProvidedCapabilities []string                           `json:"providedCapabilities,omitempty"`
}

type Query struct {
	PluginID, PluginPrefix, Namespace, Publisher, Version, Channel, Target string
	Page, PageSize                                                         int
}

type Page struct {
	Revision uint64  `json:"revision"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"pageSize"`
	Items    []Entry `json:"items"`
}

type JournalPage struct {
	Revision      uint64  `json:"revision"`
	AfterRevision uint64  `json:"afterRevision"`
	Items         []Event `json:"items"`
}

type Stats struct {
	Revision  uint64 `json:"revision"`
	Artifacts int    `json:"artifacts"`
}

type snapshot struct {
	SchemaVersion string  `json:"schemaVersion"`
	Revision      uint64  `json:"revision"`
	Items         []Entry `json:"items"`
}

type Store struct {
	root       string
	repository VerifiedRepository
	mu         sync.RWMutex
	revision   uint64
	entries    map[string]Entry
	events     []Event
}

func Open(repositoryRoot string, repository VerifiedRepository) (*Store, error) {
	if strings.TrimSpace(repositoryRoot) == "" || repository == nil {
		return nil, errors.New("Catalog 必须配置仓库根目录和已验证制品源")
	}
	store := &Store{
		root: filepath.Join(filepath.Clean(repositoryRoot), "catalog"), repository: repository,
		entries: map[string]Entry{},
	}
	for _, directory := range []string{store.root, store.journalDir()} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return nil, fmt.Errorf("准备 Catalog 私有目录: %w", err)
		}
	}
	if err := store.loadJournal(); err != nil {
		return nil, err
	}
	if err := store.rebuild(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) RecordPublished(artifact pluginservice.Artifact, attestationRaw []byte, occurredAt time.Time) (uint64, error) {
	entry, err := entryFrom(artifact, attestationRaw)
	if err != nil {
		return 0, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := refKey(entry.Ref)
	if existing, ok := s.entries[key]; ok {
		if !sameIdentity(existing, entry) {
			return 0, fmt.Errorf("Catalog 中的不可变引用 %s 与新制品不一致", key)
		}
		if err := s.writeSnapshotLocked(); err != nil {
			return 0, err
		}
		return existing.RepositoryRevision, nil
	}
	event := eventFrom(entry, occurredAt.UTC(), false)
	if err := s.appendEventLocked(&event); err != nil {
		return 0, err
	}
	entry.RepositoryRevision = event.Revision
	entry.PublishedAt = event.OccurredAt
	s.entries[key] = entry
	if err := s.writeSnapshotLocked(); err != nil {
		return 0, err
	}
	return event.Revision, nil
}

func (s *Store) Query(query Query) Page {
	s.mu.RLock()
	defer s.mu.RUnlock()
	page, pageSize := normalizePage(query.Page, query.PageSize)
	items := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		if matches(entry, query) {
			items = append(items, entry)
		}
	}
	sortEntries(items)
	total := len(items)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return Page{Revision: s.revision, Total: total, Page: page, PageSize: pageSize, Items: items[start:end]}
}

func (s *Store) Journal(afterRevision uint64, limit int) JournalPage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	items := make([]Event, 0, limit)
	for _, event := range s.events {
		if event.Revision <= afterRevision {
			continue
		}
		items = append(items, event)
		if len(items) == limit {
			break
		}
	}
	return JournalPage{Revision: s.revision, AfterRevision: afterRevision, Items: items}
}

func (s *Store) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Stats{Revision: s.revision, Artifacts: len(s.entries)}
}

func (s *Store) rebuild() error {
	refs, err := s.repository.ListRefs()
	if err != nil {
		return fmt.Errorf("枚举签名制品: %w", err)
	}
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		artifact, proof, err := s.repository.ReadMetadataWithAttestation(ref)
		if err != nil {
			return fmt.Errorf("重建 Catalog 读取 %s: %w", refKey(ref), err)
		}
		entry, err := entryFrom(artifact, proof)
		if err != nil {
			return fmt.Errorf("重建 Catalog 解析 %s: %w", refKey(ref), err)
		}
		key := refKey(ref)
		seen[key] = struct{}{}
		if prior, ok := s.entries[key]; ok {
			if !sameIdentity(prior, entry) {
				return fmt.Errorf("发布流水账与制品不一致: %s", key)
			}
			entry.RepositoryRevision = prior.RepositoryRevision
			entry.PublishedAt = prior.PublishedAt
			s.entries[key] = entry
			continue
		}
		event := eventFrom(entry, entry.SignedAt, true)
		if err := s.appendEventLocked(&event); err != nil {
			return fmt.Errorf("恢复发布流水账 %s: %w", key, err)
		}
		entry.RepositoryRevision = event.Revision
		entry.PublishedAt = event.OccurredAt
		s.entries[key] = entry
	}
	for key := range s.entries {
		if _, ok := seen[key]; !ok {
			return fmt.Errorf("发布流水账引用的制品缺失: %s", key)
		}
	}
	return s.writeSnapshotLocked()
}

func (s *Store) loadJournal() error {
	entries, err := os.ReadDir(s.journalDir())
	if err != nil {
		return fmt.Errorf("读取发布流水账: %w", err)
	}
	for _, item := range entries {
		if strings.HasPrefix(item.Name(), ".tmp-") {
			continue
		}
		if item.IsDir() || len(item.Name()) != 25 || !strings.HasSuffix(item.Name(), ".json") {
			return fmt.Errorf("发布流水账包含未知文件: %s", item.Name())
		}
		revision, err := strconv.ParseUint(strings.TrimSuffix(item.Name(), ".json"), 10, 64)
		if err != nil || revision != s.revision+1 {
			return fmt.Errorf("发布流水账 revision 不连续: %s", item.Name())
		}
		raw, err := os.ReadFile(filepath.Join(s.journalDir(), item.Name()))
		if err != nil {
			return err
		}
		var event Event
		if err := decodeStrict(raw, &event); err != nil {
			return fmt.Errorf("解析发布流水账 %s: %w", item.Name(), err)
		}
		if err := validateEvent(event, revision); err != nil {
			return fmt.Errorf("校验发布流水账 %s: %w", item.Name(), err)
		}
		key := refKey(event.Ref)
		if _, duplicate := s.entries[key]; duplicate {
			return fmt.Errorf("发布流水账重复引用: %s", key)
		}
		s.revision = revision
		s.events = append(s.events, event)
		s.entries[key] = Entry{
			Ref: event.Ref, SHA256: event.SHA256, Size: event.Size, Publisher: event.Publisher, KeyID: event.KeyID,
			SignedAt: event.SignedAt, PublishedAt: event.OccurredAt, RepositoryRevision: event.Revision,
		}
	}
	return nil
}

func (s *Store) appendEventLocked(event *Event) error {
	event.SchemaVersion = schemaVersion
	event.Revision = s.revision + 1
	event.Type = "artifact.published"
	raw, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return err
	}
	filename := filepath.Join(s.journalDir(), fmt.Sprintf("%020d.json", event.Revision))
	if err := writeNewFileAtomically(filename, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入发布流水账: %w", err)
	}
	s.revision = event.Revision
	s.events = append(s.events, *event)
	return nil
}

func (s *Store) writeSnapshotLocked() error {
	items := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		items = append(items, entry)
	}
	sortEntries(items)
	raw, err := json.MarshalIndent(snapshot{SchemaVersion: schemaVersion, Revision: s.revision, Items: items}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomically(filepath.Join(s.root, "index.json"), append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("写入 Catalog 快照: %w", err)
	}
	return nil
}

func (s *Store) journalDir() string { return filepath.Join(s.root, "journal") }

func entryFrom(artifact pluginservice.Artifact, attestationRaw []byte) (Entry, error) {
	var attestation pluginservice.Attestation
	if err := decodeStrict(attestationRaw, &attestation); err != nil {
		return Entry{}, fmt.Errorf("解析制品证明: %w", err)
	}
	if attestation.Artifact.PluginID != artifact.PluginID || attestation.Artifact.Version != artifact.Version ||
		attestation.Artifact.Channel != artifact.Channel || attestation.Artifact.SHA256 != artifact.SHA256 {
		return Entry{}, errors.New("制品证明与制品元数据不一致")
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return Entry{}, err
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		return Entry{}, err
	}
	targetSet := map[string]struct{}{}
	for target := range manifest.Engines {
		targetSet[target] = struct{}{}
	}
	for target := range manifest.Entry {
		targetSet[target] = struct{}{}
	}
	providedSet := map[string]struct{}{}
	for _, contribution := range contributions {
		providedSet[contribution.ID] = struct{}{}
	}
	if manifest.Runtime != nil {
		for _, provided := range manifest.Runtime.Provides {
			providedSet[provided.Capability] = struct{}{}
		}
	}
	providedCapabilities := make([]string, 0, len(providedSet))
	for capability := range providedSet {
		providedCapabilities = append(providedCapabilities, capability)
	}
	sort.Strings(providedCapabilities)
	targets := make([]string, 0, len(targetSet))
	for target := range targetSet {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	namespace := manifest.ID
	if last := strings.LastIndex(namespace, "."); last > 0 {
		namespace = namespace[:last]
	}
	return Entry{
		Ref:    pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel},
		SHA256: artifact.SHA256, Size: artifact.Size, Publisher: attestation.Publisher, KeyID: attestation.KeyID,
		SignedAt: attestation.SignedAt.UTC(), Name: manifest.Name, Description: manifest.Description,
		Namespace: namespace, License: manifest.License, Engines: manifest.Engines,
		Dependencies: manifest.Dependencies, Targets: targets,
		Platforms: backendPlatforms(manifest), RuntimeRequires: runtimeRequires(manifest), RuntimeProvides: runtimeProvides(manifest),
		ProvidedCapabilities: providedCapabilities,
	}, nil
}

func backendPlatforms(manifest pluginv1.Manifest) []string {
	if manifest.Execution == nil || manifest.Execution.Backend == nil {
		return nil
	}
	return append([]string(nil), manifest.Execution.Backend.Platforms...)
}

func runtimeRequires(manifest pluginv1.Manifest) []pluginv1.RuntimeRequirement {
	if manifest.Runtime == nil {
		return nil
	}
	return append([]pluginv1.RuntimeRequirement(nil), manifest.Runtime.Requires...)
}

func runtimeProvides(manifest pluginv1.Manifest) []pluginv1.RuntimeCapabilityPolicy {
	if manifest.Runtime == nil {
		return nil
	}
	return append([]pluginv1.RuntimeCapabilityPolicy(nil), manifest.Runtime.Provides...)
}

func eventFrom(entry Entry, occurredAt time.Time, recovered bool) Event {
	return Event{
		Ref: entry.Ref, SHA256: entry.SHA256, Size: entry.Size, Publisher: entry.Publisher, KeyID: entry.KeyID,
		SignedAt: entry.SignedAt, OccurredAt: occurredAt.UTC(), Recovered: recovered,
	}
}

func validateEvent(event Event, revision uint64) error {
	if event.SchemaVersion != schemaVersion || event.Revision != revision || event.Type != "artifact.published" {
		return errors.New("不支持的流水账事件")
	}
	if event.Ref.PluginID == "" || event.Ref.Version == "" || event.Ref.Channel == "" || event.Publisher == "" || event.KeyID == "" {
		return errors.New("流水账事件缺少身份字段")
	}
	digest, err := hex.DecodeString(event.SHA256)
	if err != nil || len(digest) != 32 || event.Size <= 0 || event.SignedAt.IsZero() || event.OccurredAt.IsZero() {
		return errors.New("流水账事件的摘要、大小或时间无效")
	}
	return nil
}

func sameIdentity(left, right Entry) bool {
	return left.Ref == right.Ref && left.SHA256 == right.SHA256 && left.Size == right.Size &&
		left.Publisher == right.Publisher && left.KeyID == right.KeyID && left.SignedAt.Equal(right.SignedAt)
}

func matches(entry Entry, query Query) bool {
	if query.PluginID != "" && entry.Ref.PluginID != query.PluginID {
		return false
	}
	if query.PluginPrefix != "" && entry.Ref.PluginID != query.PluginPrefix && !strings.HasPrefix(entry.Ref.PluginID, query.PluginPrefix+".") {
		return false
	}
	if query.Namespace != "" && entry.Namespace != query.Namespace {
		return false
	}
	if query.Publisher != "" && entry.Publisher != query.Publisher {
		return false
	}
	if query.Version != "" && entry.Ref.Version != query.Version {
		return false
	}
	if query.Channel != "" && entry.Ref.Channel != query.Channel {
		return false
	}
	if query.Target != "" {
		found := false
		for _, target := range entry.Targets {
			if target == query.Target {
				found = true
				break
			}
		}
		return found
	}
	return true
}

func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

func sortEntries(items []Entry) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Ref.PluginID != items[j].Ref.PluginID {
			return items[i].Ref.PluginID < items[j].Ref.PluginID
		}
		left, leftErr := semver.NewVersion(items[i].Ref.Version)
		right, rightErr := semver.NewVersion(items[j].Ref.Version)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.GreaterThan(right)
		}
		if items[i].Ref.Version != items[j].Ref.Version {
			return items[i].Ref.Version > items[j].Ref.Version
		}
		return items[i].Ref.Channel < items[j].Ref.Channel
	})
}

func refKey(ref pluginv1.ArtifactRef) string {
	return ref.PluginID + "@" + ref.Version + "/" + ref.Channel
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON 只能包含一个顶层值")
		}
		return err
	}
	return nil
}

func writeFileAtomically(filename string, data []byte, mode os.FileMode) error {
	return writeTemporaryAndCommit(filename, data, mode, true)
}

func writeNewFileAtomically(filename string, data []byte, mode os.FileMode) error {
	return writeTemporaryAndCommit(filename, data, mode, false)
}

func writeTemporaryAndCommit(filename string, data []byte, mode os.FileMode, replace bool) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(filename), ".tmp-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	written, writeErr := file.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if !replace {
		if err := os.Link(temporary, filename); err != nil {
			return err
		}
		return nil
	}
	return os.Rename(temporary, filename)
}

func ensurePrivateDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s 必须是普通目录且不能是符号链接", directory)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s 权限过宽 %o，要求 0700 或更严格", directory, info.Mode().Perm())
	}
	return nil
}
