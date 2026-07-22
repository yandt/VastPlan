package broker

import (
	"errors"
	"sync"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
)

// AssertionStore is owned by the leader-routed Broker. Keeping consumption at
// the issuer makes replay protection consistent across every Portal Node.
type AssertionStore interface {
	Issue(authenticationv1.AuthenticationAssertion) error
	Consume(assertionID string, expiresAt time.Time) bool
}

type MemoryAssertionStore struct {
	mu     sync.Mutex
	max    int
	values map[string]time.Time
	now    func() time.Time
}

func NewMemoryAssertionStore(max int) *MemoryAssertionStore {
	if max <= 0 {
		max = 4096
	}
	return &MemoryAssertionStore{max: max, values: map[string]time.Time{}, now: func() time.Time { return time.Now().UTC() }}
}

func (s *MemoryAssertionStore) Issue(value authenticationv1.AuthenticationAssertion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if _, exists := s.values[value.AssertionID]; exists {
		return errors.New("Authentication Assertion ID 已存在")
	}
	if len(s.values) >= s.max {
		return errors.New("Authentication Assertion 容量已满")
	}
	s.values[value.AssertionID] = value.ExpiresAt
	return nil
}

func (s *MemoryAssertionStore) Consume(assertionID string, expiresAt time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	issuedExpiry, exists := s.values[assertionID]
	if !exists || !issuedExpiry.Equal(expiresAt) {
		return false
	}
	delete(s.values, assertionID)
	return true
}

func (s *MemoryAssertionStore) prune() {
	now := s.now()
	for id, expiresAt := range s.values {
		if !now.Before(expiresAt) {
			delete(s.values, id)
		}
	}
}
