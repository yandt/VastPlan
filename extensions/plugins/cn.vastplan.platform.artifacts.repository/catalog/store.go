// Package catalog implements the managed repository's derived artifact catalog
// and append-only publish journal. Trust and immutable storage remain owned by
// pluginservice; this package only indexes artifacts that the signed repository
// has already verified.
package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
)

const schemaVersion = "v1"

type VerifiedRepository interface {
	ListRefs() ([]pluginservice.Ref, error)
	ReadMetadataWithAttestation(pluginservice.Ref) (pluginservice.Artifact, []byte, error)
}

type VerifiedProvenanceReader interface {
	ReadProvenance(pluginservice.Ref) ([]byte, []byte, error)
}

type VerifiedSecurityAdmissionReader interface {
	ReadSecurityAdmission(pluginservice.Ref) ([]byte, error)
}

type VerifiedMetadataProvenanceReader interface {
	ReadMetadataWithProvenance(pluginservice.Ref) (pluginservice.Artifact, []byte, []byte, []byte, error)
}

type VerifiedMetadataSupplyChainReader interface {
	ReadMetadataWithSupplyChain(pluginservice.Ref) (pluginservice.Artifact, []byte, []byte, []byte, []byte, error)
}

// MissingArtifactRegistry allows Catalog history to retain an exact artifact
// after its bytes have entered the crash-recoverable GC retirement state.
// Unknown physical loss must still fail repository startup.
type MissingArtifactRegistry interface {
	AllowsMissing(pluginv1.ArtifactRef, string) bool
}

type Event struct {
	SchemaVersion  string                        `json:"schemaVersion"`
	Revision       uint64                        `json:"revision"`
	Type           string                        `json:"type"`
	Ref            pluginv1.ArtifactRef          `json:"ref"`
	SHA256         string                        `json:"sha256"`
	Size           int64                         `json:"size"`
	Publisher      string                        `json:"publisher"`
	KeyID          string                        `json:"keyId"`
	SignedAt       time.Time                     `json:"signedAt"`
	OccurredAt     time.Time                     `json:"occurredAt"`
	Recovered      bool                          `json:"recovered,omitempty"`
	PreviousStatus string                        `json:"previousStatus,omitempty"`
	Status         string                        `json:"status,omitempty"`
	Reason         string                        `json:"reason,omitempty"`
	Replacement    *pluginv1.ArtifactRequirement `json:"replacement,omitempty"`
}

type Entry struct {
	Ref                  pluginv1.ArtifactRef                                   `json:"ref"`
	SHA256               string                                                 `json:"sha256"`
	Size                 int64                                                  `json:"size"`
	Publisher            string                                                 `json:"publisher"`
	KeyID                string                                                 `json:"keyId"`
	SignedAt             time.Time                                              `json:"signedAt"`
	PublishedAt          time.Time                                              `json:"publishedAt"`
	RepositoryRevision   uint64                                                 `json:"repositoryRevision"`
	Name                 string                                                 `json:"name"`
	Description          string                                                 `json:"description"`
	Namespace            string                                                 `json:"namespace"`
	License              string                                                 `json:"license,omitempty"`
	Engines              map[string]string                                      `json:"engines"`
	Dependencies         map[string]string                                      `json:"dependencies,omitempty"`
	Targets              []string                                               `json:"targets"`
	Platforms            []string                                               `json:"platforms,omitempty"`
	RuntimeRequires      []pluginv1.RuntimeRequirement                          `json:"runtimeRequires,omitempty"`
	RuntimeProvides      []pluginv1.RuntimeCapabilityPolicy                     `json:"runtimeProvides,omitempty"`
	ProvidedCapabilities []string                                               `json:"providedCapabilities,omitempty"`
	SBOM                 *platformadminapi.ArtifactSBOMDeclaration              `json:"sbom,omitempty"`
	PythonLock           *platformadminapi.ArtifactPythonLockDeclaration        `json:"pythonLock,omitempty"`
	Provenance           *platformadminapi.ArtifactProvenanceDeclaration        `json:"provenance,omitempty"`
	SecurityAdmission    *platformadminapi.ArtifactSecurityAdmissionDeclaration `json:"securityAdmission,omitempty"`
	SecurityStatus       *platformadminapi.ArtifactSecurityStatusEvidence       `json:"securityStatus,omitempty"`
	LifecycleStatus      string                                                 `json:"lifecycleStatus"`
	LifecycleRevision    uint64                                                 `json:"lifecycleRevision,omitempty"`
	LifecycleReason      string                                                 `json:"lifecycleReason,omitempty"`
	Replacement          *pluginv1.ArtifactRequirement                          `json:"replacement,omitempty"`
}

type Query struct {
	PluginID, PluginPrefix, Namespace, Publisher, Version, Channel, Target, Lifecycle string
	Page, PageSize                                                                    int
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
	Revision                   uint64 `json:"revision"`
	Artifacts                  int    `json:"artifacts"`
	InventorySHA256            string `json:"inventorySHA256"`
	PublicationRevision        uint64 `json:"publicationRevision"`
	PublicationInventorySHA256 string `json:"publicationInventorySHA256"`
}

type snapshot struct {
	SchemaVersion string  `json:"schemaVersion"`
	Revision      uint64  `json:"revision"`
	Items         []Entry `json:"items"`
}

type Store struct {
	root                string
	repository          VerifiedRepository
	mu                  sync.RWMutex
	revision            uint64
	entries             map[string]Entry
	events              []Event
	lifecycle           map[string][]LifecycleTransition
	retired             MissingArtifactRegistry
	publicationRevision uint64
	publications        map[string]Publication
}

func Open(repositoryRoot string, repository VerifiedRepository, retired ...MissingArtifactRegistry) (*Store, error) {
	if strings.TrimSpace(repositoryRoot) == "" || repository == nil {
		return nil, errors.New("Catalog 必须配置仓库根目录和已验证制品源")
	}
	store := &Store{
		root: filepath.Join(filepath.Clean(repositoryRoot), "catalog"), repository: repository,
		entries: map[string]Entry{}, lifecycle: map[string][]LifecycleTransition{}, publications: map[string]Publication{},
	}
	if len(retired) > 1 {
		return nil, errors.New("Catalog 只能配置一个制品 retirement 注册表")
	}
	if len(retired) == 1 {
		store.retired = retired[0]
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
	if err := store.loadPublications(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) RecordPublished(artifact pluginservice.Artifact, attestationRaw []byte, occurredAt time.Time) (uint64, error) {
	entry, err := entryFrom(artifact, attestationRaw)
	if err != nil {
		return 0, err
	}
	if err := s.enrichProvenance(&entry); err != nil {
		return 0, err
	}
	if err := s.enrichSecurityAdmission(&entry); err != nil {
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
	entry.LifecycleStatus = LifecycleActive
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
	return Stats{Revision: s.revision, Artifacts: len(s.entries), InventorySHA256: inventoryDigest(s.entries), PublicationRevision: s.publicationRevision, PublicationInventorySHA256: publicationInventoryDigest(s.publications)}
}

func (s *Store) Lookup(ref pluginv1.ArtifactRef) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[refKey(ref)]
	return entry, ok
}

func (s *Store) Entries() (uint64, []Entry) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		items = append(items, entry)
	}
	sortEntries(items)
	return s.revision, items
}

// GarbageCandidates returns only administratively retired artifacts. Active
// and deprecated entries remain resolvable and can never become implicit GC.
func (s *Store) GarbageCandidates() (uint64, []Entry) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Entry, 0)
	for _, entry := range s.entries {
		if entry.LifecycleStatus == LifecycleYanked || entry.LifecycleStatus == LifecycleRevoked {
			items = append(items, entry)
		}
	}
	sortEntries(items)
	return s.revision, items
}

func (s *Store) GarbageCandidate(ref pluginv1.ArtifactRef, sha256 string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[refKey(ref)]
	if !ok || entry.SHA256 != sha256 || (entry.LifecycleStatus != LifecycleYanked && entry.LifecycleStatus != LifecycleRevoked) {
		return Entry{}, false
	}
	return entry, true
}

func inventoryDigest(entries map[string]Entry) string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		entry := entries[key]
		_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%d\x00%s\x00%d\n", key, entry.SHA256, entry.RepositoryRevision, entry.LifecycleStatus, entry.LifecycleRevision)
	}
	return hex.EncodeToString(hash.Sum(nil))
}
