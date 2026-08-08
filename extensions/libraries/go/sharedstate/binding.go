package sharedstate

import (
	"context"
	"errors"
	"sync"
)

// BindingStore is the kernel's stable state.shared.v1 port across two-stage
// bootstrap. Before a durable provider profile is committed it reports the
// distinct unconfigured state. Once RequireProvider or Bind is called it can
// only move forward and never falls back to the bootstrap state, preventing
// split-brain persistence after an external provider has failed.
type BindingStore struct {
	mu         sync.RWMutex
	required   bool
	pending    uint64
	generation uint64
	identity   string
	store      Store
	// live tracks whether the bound provider is currently reachable. It is
	// derived from call outcomes rather than probed separately: ErrUnavailable
	// is the only error that means the provider could not be reached, while
	// application errors prove it answered. This is deliberately distinct from
	// store != nil, which only records that a binding once succeeded.
	live    bool
	changes chan struct{}
}

func NewBindingStore() *BindingStore { return &BindingStore{changes: make(chan struct{}, 1)} }

// RequireProvider records the durable decision that this binding must use an
// external provider. The transition is intentionally irreversible for the
// lifetime of the kernel process.
func (s *BindingStore) RequireProvider() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.required = true
	s.mu.Unlock()
}

// BeginProviderCommit closes the bootstrap fallback while a durable profile
// commit is in flight. The returned completion must be called exactly once;
// successful commits make the requirement permanent, while failed commits
// restore unconfigured only when no other commit is pending.
func (s *BindingStore) BeginProviderCommit() func(bool) {
	if s == nil {
		return func(bool) {}
	}
	s.mu.Lock()
	s.pending++
	s.mu.Unlock()
	var once sync.Once
	return func(committed bool) {
		once.Do(func() {
			s.mu.Lock()
			if s.pending > 0 {
				s.pending--
			}
			if committed {
				s.required = true
			}
			s.mu.Unlock()
		})
	}
}

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
	s.required = true
	s.generation, s.identity, s.store = generation, identity, store
	// The caller only binds a store it just opened successfully.
	s.live = true
	select {
	case s.changes <- struct{}{}:
	default:
	}
	return nil
}

// observe folds a call outcome into the liveness signal. Application errors
// (not found, conflict, invalid) prove the provider answered, so only
// ErrUnavailable clears it. A later successful call restores it, which is what
// lets a gate reopen without waiting for a rebind.
func (s *BindingStore) observe(err error) {
	if s == nil {
		return
	}
	reachable := !errors.Is(err, ErrUnavailable)
	s.mu.Lock()
	if s.store != nil {
		s.live = reachable
	}
	s.mu.Unlock()
}

// Live reports whether the bound provider is currently usable. Callers that
// gate work on the provider actually answering must use this instead of
// Snapshot, whose readiness flag never falls back once a binding succeeded.
func (s *BindingStore) Live() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store != nil && s.live
}

// Changes reports successful generation switches. It is an edge-triggered
// wake-up hint: consumers must re-read Snapshot and must not derive state from
// the number of received events.
func (s *BindingStore) Changes() <-chan struct{} {
	if s == nil {
		return nil
	}
	return s.changes
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
	store, required := s.store, s.required || s.pending > 0
	s.mu.RUnlock()
	if store == nil {
		if !required {
			return nil, ErrUnconfigured
		}
		return nil, ErrUnavailable
	}
	return store, nil
}

func (s *BindingStore) Get(ctx context.Context, scope Scope, key string) (Entry, error) {
	store, err := s.current()
	if err != nil {
		return Entry{}, err
	}
	entry, err := store.Get(ctx, scope, key)
	s.observe(err)
	return entry, err
}

func (s *BindingStore) Create(ctx context.Context, scope Scope, key string, value []byte) (Entry, error) {
	store, err := s.current()
	if err != nil {
		return Entry{}, err
	}
	entry, err := store.Create(ctx, scope, key, value)
	s.observe(err)
	return entry, err
}

func (s *BindingStore) Update(ctx context.Context, scope Scope, key string, value []byte, expected uint64) (Entry, error) {
	store, err := s.current()
	if err != nil {
		return Entry{}, err
	}
	entry, err := store.Update(ctx, scope, key, value, expected)
	s.observe(err)
	return entry, err
}

func (s *BindingStore) Delete(ctx context.Context, scope Scope, key string, expected uint64) error {
	store, err := s.current()
	if err != nil {
		return err
	}
	err = store.Delete(ctx, scope, key, expected)
	s.observe(err)
	return err
}

func (s *BindingStore) List(ctx context.Context, scope Scope, prefix string, limit int, cursor string) (Page, error) {
	store, err := s.current()
	if err != nil {
		return Page{}, err
	}
	page, err := store.List(ctx, scope, prefix, limit, cursor)
	s.observe(err)
	return page, err
}

var _ Store = (*BindingStore)(nil)

func IsUnavailable(err error) bool { return errors.Is(err, ErrUnavailable) }

func IsUnconfigured(err error) bool { return errors.Is(err, ErrUnconfigured) }
