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

const schemaVersion = 1

type persistedSnapshot struct {
	SchemaVersion int                                         `json:"schemaVersion"`
	Catalog       backendcompositionv1.BackendPlatformCatalog `json:"catalog"`
	Digest        string                                      `json:"digest"`
}

// Store implements deploymentpublisher.CatalogSource without importing that
// package. Seed is an immutable, already validated startup fallback; it is not
// silently persisted with the Manager Node's runtime credentials.
type Store struct {
	KV        jetstream.KeyValue
	key       string
	catalogID string
	seed      backendcompositionv1.BackendPlatformCatalog
}

func NewStore(kv jetstream.KeyValue, seed backendcompositionv1.BackendPlatformCatalog) (*Store, error) {
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
	return &Store{KV: kv, key: sharedcontrolplane.BackendPlatformCatalogKey(validated.ID), catalogID: validated.ID, seed: validated}, nil
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

// Seed persists the initial snapshot with create-only semantics. It is intended
// for the privileged control-plane/bootstrap identity, never the Manager Node.
// Existing state is validated and retained even when a newer startup file is
// supplied; online revisions remain the authority after first publication.
func (s *Store) Seed(ctx context.Context) (uint64, error) {
	if s == nil || s.KV == nil || s.key == "" {
		return 0, errors.New("Backend Platform Catalog Store 未初始化")
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
	return entry.Revision(), nil
}

func encodeSnapshot(catalog backendcompositionv1.BackendPlatformCatalog) ([]byte, error) {
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(catalog)
	if err != nil {
		return nil, err
	}
	return json.Marshal(persistedSnapshot{SchemaVersion: schemaVersion, Catalog: validated, Digest: validated.Digest()})
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
	if snapshot.SchemaVersion != schemaVersion {
		return persistedSnapshot{}, errors.New("Backend Platform Catalog 快照版本无效")
	}
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(snapshot.Catalog)
	if err != nil || snapshot.Digest != validated.Digest() {
		return persistedSnapshot{}, errors.New("Backend Platform Catalog 快照摘要无效")
	}
	snapshot.Catalog = validated
	return snapshot, nil
}

func cloneCatalog(catalog backendcompositionv1.BackendPlatformCatalog) backendcompositionv1.BackendPlatformCatalog {
	raw, _ := json.Marshal(catalog)
	var out backendcompositionv1.BackendPlatformCatalog
	_ = json.Unmarshal(raw, &out)
	return out
}
