package broker

import (
	"errors"
	"sync"
	"time"
)

type TransactionRoute struct {
	ProviderID string
	ProfileID  string
	MethodID   string
	TenantID   string
	PortalID   string
	Audience   string
	ExpiresAt  time.Time
}

type TransactionStore interface {
	Put(transactionID string, route TransactionRoute) error
	Get(transactionID string) (TransactionRoute, bool)
	Delete(transactionID string)
}

type MemoryTransactionStore struct {
	mu     sync.Mutex
	max    int
	values map[string]TransactionRoute
	now    func() time.Time
}

func NewMemoryTransactionStore(max int) *MemoryTransactionStore {
	if max <= 0 {
		max = 4096
	}
	return &MemoryTransactionStore{max: max, values: map[string]TransactionRoute{}, now: func() time.Time { return time.Now().UTC() }}
}

func (s *MemoryTransactionStore) Put(id string, route TransactionRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if _, exists := s.values[id]; !exists && len(s.values) >= s.max {
		return errors.New("Authentication Broker transaction 容量已满")
	}
	s.values[id] = route
	return nil
}

func (s *MemoryTransactionStore) Get(id string) (TransactionRoute, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	route, ok := s.values[id]
	return route, ok
}

func (s *MemoryTransactionStore) Delete(id string) { s.mu.Lock(); delete(s.values, id); s.mu.Unlock() }

func (s *MemoryTransactionStore) prune() {
	now := s.now()
	for id, route := range s.values {
		if !now.Before(route.ExpiresAt) {
			delete(s.values, id)
		}
	}
}
