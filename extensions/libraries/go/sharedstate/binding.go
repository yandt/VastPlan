package sharedstate

import (
	"context"
	"errors"
	"sync"
)

// BindingStore is the kernel's stable state.shared.v1 port across two-stage
// bootstrap. It starts unavailable and can only move to a newer provider
// generation. It never falls back to a local file after an external provider
// has failed, preventing split-brain state.
type BindingStore struct {
	mu         sync.RWMutex
	generation uint64
	identity   string
	store      Store
}

func NewBindingStore() *BindingStore { return &BindingStore{} }

func (s *BindingStore) Bind(generation uint64, identity string, store Store) error {
	if s == nil || generation == 0 || identity == "" || store == nil {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation < s.generation {
		return ErrConflict
	}
	if generation == s.generation {
		if identity != s.identity {
			return ErrConflict
		}
		return nil
	}
	s.generation, s.identity, s.store = generation, identity, store
	return nil
}

func (s *BindingStore) Snapshot() (uint64, string, bool) {
	if s == nil {
		return 0, "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation, s.identity, s.store != nil
}

func (s *BindingStore) current() (Store, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()
	if store == nil {
		return nil, ErrUnavailable
	}
	return store, nil
}

func (s *BindingStore) Get(ctx context.Context, scope Scope, key string) (Entry, error) {
	store, err := s.current()
	if err != nil {
		return Entry{}, err
	}
	return store.Get(ctx, scope, key)
}

func (s *BindingStore) Create(ctx context.Context, scope Scope, key string, value []byte) (Entry, error) {
	store, err := s.current()
	if err != nil {
		return Entry{}, err
	}
	return store.Create(ctx, scope, key, value)
}

func (s *BindingStore) Update(ctx context.Context, scope Scope, key string, value []byte, expected uint64) (Entry, error) {
	store, err := s.current()
	if err != nil {
		return Entry{}, err
	}
	return store.Update(ctx, scope, key, value, expected)
}

func (s *BindingStore) Delete(ctx context.Context, scope Scope, key string, expected uint64) error {
	store, err := s.current()
	if err != nil {
		return err
	}
	return store.Delete(ctx, scope, key, expected)
}

func (s *BindingStore) List(ctx context.Context, scope Scope, prefix string, limit int, cursor string) (Page, error) {
	store, err := s.current()
	if err != nil {
		return Page{}, err
	}
	return store.List(ctx, scope, prefix, limit, cursor)
}

var _ Store = (*BindingStore)(nil)

func IsUnavailable(err error) bool { return errors.Is(err, ErrUnavailable) }
