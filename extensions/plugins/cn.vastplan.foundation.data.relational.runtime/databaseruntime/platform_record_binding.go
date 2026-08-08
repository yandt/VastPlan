package databaseruntime

import (
	"context"
	"errors"
	"sync"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

// PlatformRecordStore is the trusted, non-exportable data path from the
// two-stage Platform Control bootstrap into Record Store. It deliberately
// exposes sessions rather than a pool, credential, DSN or driver object.
type PlatformRecordStore interface {
	ProviderID() string
	Read(context.Context, func(recordstore.Session) error) error
	Write(context.Context, func(recordstore.Session) error) error
	Begin(context.Context, databasev1.TransactionOptions) (Transaction, error)
	WithPinned(context.Context, func(PinnedSession) error) error
	Closed() <-chan struct{}
}

type platformRecordSnapshot struct {
	generation uint64
	identity   string
	store      PlatformRecordStore
}

func (s platformRecordSnapshot) connection() databasev1.ConnectionRef {
	return databasev1.ConnectionRef{ResourceID: platformcontrolv1.DatabaseConnectionResourceID, Revision: s.generation}
}

type platformTransactionResource struct{ snapshot platformRecordSnapshot }

func (r platformTransactionResource) ProviderID() string { return r.snapshot.store.ProviderID() }
func (r platformTransactionResource) Begin(ctx context.Context, options databasev1.TransactionOptions) (Transaction, error) {
	return r.snapshot.store.Begin(ctx, options)
}
func (r platformTransactionResource) Closed() <-chan struct{} { return r.snapshot.store.Closed() }
func (platformTransactionResource) Release()                  {}

// PlatformRecordBinding follows the same monotonic generation rule as the
// Shared State binding. A failed external store never falls back to local JSON.
type PlatformRecordBinding struct {
	mu         sync.RWMutex
	generation uint64
	identity   string
	store      PlatformRecordStore
}

func NewPlatformRecordBinding() *PlatformRecordBinding { return &PlatformRecordBinding{} }

func (b *PlatformRecordBinding) Bind(generation uint64, identity string, store PlatformRecordStore) error {
	if b == nil || generation == 0 || identity == "" || store == nil || store.ProviderID() == "" {
		return errors.New("Platform Record Store binding 无效")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if generation < b.generation || generation == b.generation && identity != b.identity {
		return recordstore.ErrConflict
	}
	if generation == b.generation {
		return nil
	}
	b.generation, b.identity, b.store = generation, identity, store
	return nil
}

func (b *PlatformRecordBinding) Snapshot() (uint64, string, bool) {
	if b == nil {
		return 0, "", false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.generation, b.identity, b.store != nil
}

func (b *PlatformRecordBinding) current() (platformRecordSnapshot, error) {
	if b == nil {
		return platformRecordSnapshot{}, recordstore.ErrStorageUnavailable
	}
	b.mu.RLock()
	snapshot := platformRecordSnapshot{generation: b.generation, identity: b.identity, store: b.store}
	b.mu.RUnlock()
	if snapshot.store == nil {
		return platformRecordSnapshot{}, recordstore.ErrStorageUnavailable
	}
	return snapshot, nil
}
