// Package platformcatalog persists the trusted active Backend Platform Catalog
// snapshot. It does not expose candidate mutation yet; writes are deliberately
// separated from the Manager Node's read-only runtime identity.
package platformcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/nats-io/nats.go/jetstream"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

const schemaVersion = 3

type persistedSnapshot struct {
	SchemaVersion int                                         `json:"schemaVersion"`
	Catalog       backendcompositionv1.BackendPlatformCatalog `json:"catalog"`
	Digest        string                                      `json:"digest"`
	Candidate     *Candidate                                  `json:"candidate,omitempty"`
}

// Store implements deploymentpublisher.CatalogSource without importing that
// package. Seed is an immutable, already validated startup fallback; it is not
// silently persisted with the Manager Node's runtime credentials.
type Store struct {
	KV        jetstream.KeyValue
	writer    jetstream.KeyValue
	key       string
	catalogID string
	seed      backendcompositionv1.BackendPlatformCatalog
}

func NewStore(kv jetstream.KeyValue, seed backendcompositionv1.BackendPlatformCatalog) (*Store, error) {
	return newStore(kv, nil, seed)
}

// NewWritableStore keeps reads on the Manager Node's read-only identity while
// candidate CAS writes use the dedicated catalog-publisher identity.
func NewWritableStore(readKV, writeKV jetstream.KeyValue, seed backendcompositionv1.BackendPlatformCatalog) (*Store, error) {
	if writeKV == nil {
		return nil, errors.New("Backend Platform Catalog Publisher KV 未配置")
	}
	return newStore(readKV, writeKV, seed)
}

func newStore(kv, writer jetstream.KeyValue, seed backendcompositionv1.BackendPlatformCatalog) (*Store, error) {
	if kv == nil {
		return nil, errors.New("Backend Platform Catalog KV 未配置")
	}
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(seed)
	if err != nil {
		return nil, fmt.Errorf("校验 Backend Platform Catalog Seed: %w", err)
	}
	if strings.TrimSpace(validated.ID) == "" {
		return nil, errors.New("Backend Platform Catalog Seed ID 为空")
	}
	return &Store{KV: kv, writer: writer, key: sharedcontrolplane.BackendPlatformCatalogKey(validated.ID), catalogID: validated.ID, seed: validated}, nil
}

// Snapshot returns the durable active snapshot when present. A missing key
// falls back to the startup Seed so an already deployed service remains
// manageable while the privileged bootstrap publisher is temporarily absent.
// Corrupt or identity-mismatched durable state never falls back silently.
func (s *Store) Snapshot(ctx context.Context) (backendcompositionv1.BackendPlatformCatalog, error) {
	if s == nil || s.KV == nil || s.key == "" {
		return backendcompositionv1.BackendPlatformCatalog{}, errors.New("Backend Platform Catalog Store 未初始化")
	}
	entry, err := s.KV.Get(ctx, s.key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return cloneCatalog(s.seed), nil
	}
	if err != nil {
		return backendcompositionv1.BackendPlatformCatalog{}, fmt.Errorf("读取 Backend Platform Catalog 快照: %w", err)
	}
	snapshot, err := parseSnapshot(entry.Value())
	if err != nil {
		return backendcompositionv1.BackendPlatformCatalog{}, fmt.Errorf("持久 Backend Platform Catalog 快照损坏: %w", err)
	}
	if snapshot.Catalog.ID != s.catalogID {
		return backendcompositionv1.BackendPlatformCatalog{}, errors.New("持久 Backend Platform Catalog 身份与 Seed 不匹配")
	}
	return cloneCatalog(snapshot.Catalog), nil
}

// SnapshotForBinding is the publication fence used by ordinary Application
// publication. A candidate locks only its exact tenant/deployment binding;
// unrelated deployments continue to use the active Catalog.
func (s *Store) SnapshotForBinding(ctx context.Context, tenantID, deploymentName string) (backendcompositionv1.BackendPlatformCatalog, error) {
	snapshot, err := s.readPersisted(ctx)
	if err != nil {
		return backendcompositionv1.BackendPlatformCatalog{}, err
	}
	if snapshot.Candidate != nil && snapshot.Candidate.locks(tenantID, deploymentName) {
		return backendcompositionv1.BackendPlatformCatalog{}, ErrBindingLocked
	}
	return cloneCatalog(snapshot.Catalog), nil
}

// SnapshotForCandidate is deliberately separate from SnapshotForBinding. It
// lets the future Profile Activation controller publish the exact activated
// candidate without creating a general lock bypass for Application callers.
func (s *Store) SnapshotForCandidate(ctx context.Context, candidateID, requestDigest string) (backendcompositionv1.BackendPlatformCatalog, error) {
	snapshot, err := s.readPersisted(ctx)
	if err != nil {
		return backendcompositionv1.BackendPlatformCatalog{}, err
	}
	candidate, err := requireCandidate(snapshot.Candidate, candidateID, requestDigest)
	if err != nil {
		return backendcompositionv1.BackendPlatformCatalog{}, err
	}
	if candidate.Status != CandidateActivated || snapshot.Digest != candidate.NextCatalogDigest {
		return backendcompositionv1.BackendPlatformCatalog{}, ErrInvalidTransition
	}
	return cloneCatalog(snapshot.Catalog), nil
}

// SnapshotForCandidatePreview deterministically materializes the candidate
// Catalog while it is still Prepared. It does not change the active snapshot
// and is intentionally unavailable through the ordinary publication port.
func (s *Store) SnapshotForCandidatePreview(ctx context.Context, candidateID, requestDigest string) (backendcompositionv1.BackendPlatformCatalog, error) {
	snapshot, err := s.readPersisted(ctx)
	if err != nil {
		return backendcompositionv1.BackendPlatformCatalog{}, err
	}
	candidate, err := requireCandidate(snapshot.Candidate, candidateID, requestDigest)
	if err != nil {
		return backendcompositionv1.BackendPlatformCatalog{}, err
	}
	switch candidate.Status {
	case CandidatePrepared:
		next, _, err := buildCandidateCatalog(snapshot.Catalog, candidate.prepareRequest())
		if err != nil || next.Digest() != candidate.NextCatalogDigest {
			return backendcompositionv1.BackendPlatformCatalog{}, ErrCatalogConflict
		}
		return next, nil
	case CandidateActivated, CandidateFinalized:
		if snapshot.Digest != candidate.NextCatalogDigest {
			return backendcompositionv1.BackendPlatformCatalog{}, ErrCatalogConflict
		}
		return cloneCatalog(snapshot.Catalog), nil
	default:
		return backendcompositionv1.BackendPlatformCatalog{}, ErrInvalidTransition
	}
}

// Seed persists the initial snapshot with create-only semantics. It is intended
// for the privileged control-plane/bootstrap identity, never the Manager Node.
// Existing state is validated and retained even when a newer startup file is
// supplied; online revisions remain the authority after first publication.
func (s *Store) Seed(ctx context.Context) (uint64, error) {
	return s.persistSeed(ctx, 0)
}

func (s *Store) persistSeed(ctx context.Context, attempt int) (uint64, error) {
	if s == nil || s.KV == nil || s.key == "" {
		return 0, errors.New("Backend Platform Catalog Store 未初始化")
	}
	if attempt >= maxCASAttempts {
		return 0, errors.New("升级 Backend Platform Catalog Seed 的 CAS 竞争过多")
	}
	raw, err := encodeSnapshot(s.seed)
	if err != nil {
		return 0, err
	}
	revision, err := s.KV.Create(ctx, s.key, raw)
	if err == nil {
		return revision, nil
	}
	if !errors.Is(err, jetstream.ErrKeyExists) {
		return 0, fmt.Errorf("创建 Backend Platform Catalog Seed: %w", err)
	}
	entry, getErr := s.KV.Get(ctx, s.key)
	if getErr != nil {
		return 0, fmt.Errorf("重读已存在 Backend Platform Catalog: %w", getErr)
	}
	existing, parseErr := parseSnapshot(entry.Value())
	if parseErr != nil || existing.Catalog.ID != s.catalogID {
		return 0, errors.New("已存在 Backend Platform Catalog 损坏或身份不匹配")
	}
	normalized, encodeErr := encodeState(existing)
	if encodeErr != nil {
		return 0, errors.New("已存在 Backend Platform Catalog 无法规范化")
	}
	if !bytes.Equal(bytes.TrimSpace(entry.Value()), normalized) {
		revision, updateErr := s.KV.Update(ctx, s.key, normalized, entry.Revision())
		if updateErr == nil {
			return revision, nil
		}
		if errors.Is(updateErr, jetstream.ErrKeyExists) {
			return s.persistSeed(ctx, attempt+1)
		}
		return 0, fmt.Errorf("升级 Backend Platform Catalog Seed: %w", updateErr)
	}
	return entry.Revision(), nil
}

func (s *Store) readPersisted(ctx context.Context) (persistedSnapshot, error) {
	if s == nil || s.KV == nil || s.key == "" {
		return persistedSnapshot{}, errors.New("Backend Platform Catalog Store 未初始化")
	}
	entry, err := s.KV.Get(ctx, s.key)
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return persistedSnapshot{}, ErrCatalogNotSeeded
	}
	if err != nil {
		return persistedSnapshot{}, fmt.Errorf("读取 Backend Platform Catalog 快照: %w", err)
	}
	snapshot, err := parseSnapshot(entry.Value())
	if err != nil {
		return persistedSnapshot{}, fmt.Errorf("持久 Backend Platform Catalog 快照损坏: %w", err)
	}
	if snapshot.Catalog.ID != s.catalogID {
		return persistedSnapshot{}, errors.New("持久 Backend Platform Catalog 身份与 Seed 不匹配")
	}
	return snapshot, nil
}

func encodeSnapshot(catalog backendcompositionv1.BackendPlatformCatalog) ([]byte, error) {
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(catalog)
	if err != nil {
		return nil, err
	}
	return encodeState(persistedSnapshot{SchemaVersion: schemaVersion, Catalog: validated, Digest: validated.Digest()})
}

func encodeState(snapshot persistedSnapshot) ([]byte, error) {
	validated, err := validateSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(validated)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > sharedcontrolplane.MaxDesiredStateBytes {
		return nil, errors.New("Backend Platform Catalog 快照大小无效")
	}
	return raw, nil
}

func parseSnapshot(raw []byte) (persistedSnapshot, error) {
	if len(raw) == 0 || len(raw) > sharedcontrolplane.MaxDesiredStateBytes {
		return persistedSnapshot{}, errors.New("Backend Platform Catalog 快照大小无效")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var snapshot persistedSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return persistedSnapshot{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return persistedSnapshot{}, errors.New("Backend Platform Catalog 快照包含多余内容")
	}
	return validateSnapshot(snapshot)
}

func validateSnapshot(snapshot persistedSnapshot) (persistedSnapshot, error) {
	if snapshot.SchemaVersion == 1 || snapshot.SchemaVersion == 2 {
		if snapshot.Candidate != nil {
			return persistedSnapshot{}, errors.New("旧版 Backend Platform Catalog 快照不得包含候选")
		}
		snapshot.SchemaVersion = schemaVersion
	} else if snapshot.SchemaVersion != schemaVersion {
		return persistedSnapshot{}, errors.New("Backend Platform Catalog 快照版本无效")
	}
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(snapshot.Catalog)
	if err != nil || snapshot.Digest != validated.Digest() {
		return persistedSnapshot{}, errors.New("Backend Platform Catalog 快照摘要无效")
	}
	snapshot.Catalog = validated
	if err := validateCandidateAgainstSnapshot(snapshot); err != nil {
		return persistedSnapshot{}, err
	}
	return snapshot, nil
}

func cloneCatalog(catalog backendcompositionv1.BackendPlatformCatalog) backendcompositionv1.BackendPlatformCatalog {
	raw, _ := json.Marshal(catalog)
	var out backendcompositionv1.BackendPlatformCatalog
	_ = json.Unmarshal(raw, &out)
	return out
}
