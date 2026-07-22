package otpprovider

import (
	"errors"
	"sync"
	"time"
)

type challengePhase string

const (
	phaseIdentifier challengePhase = "identifier"
	phaseDelivering challengePhase = "delivering"
	phaseCode       challengePhase = "code"
)

type challenge struct {
	Revision   uint64
	Phase      challengePhase
	ProfileID  string
	MethodID   string
	StepID     string
	Identifier string
	SubjectID  string
	Locale     string
	CodeMAC    []byte
	ExpiresAt  time.Time
	ResendAt   time.Time
	Attempts   int
	Resends    int
}

type ChallengeStore interface {
	Create(string, challenge) error
	Load(string) (challenge, bool)
	CompareAndSwap(string, uint64, challenge) bool
	Consume(string, uint64) bool
	Delete(string)
}

type MemoryChallengeStore struct {
	mu     sync.Mutex
	max    int
	values map[string]challenge
	now    func() time.Time
}

func NewMemoryChallengeStore(max int) *MemoryChallengeStore {
	if max <= 0 {
		max = 4096
	}
	return &MemoryChallengeStore{max: max, values: map[string]challenge{}, now: func() time.Time { return time.Now().UTC() }}
}

func (s *MemoryChallengeStore) Create(id string, value challenge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if _, exists := s.values[id]; exists {
		return errors.New("OTP transaction 已存在")
	}
	if len(s.values) >= s.max {
		return errors.New("OTP transaction 容量已满")
	}
	value.Revision = 1
	s.values[id] = cloneChallenge(value)
	return nil
}
func (s *MemoryChallengeStore) Load(id string) (challenge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	value, ok := s.values[id]
	return cloneChallenge(value), ok
}
func (s *MemoryChallengeStore) CompareAndSwap(id string, revision uint64, value challenge) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	current, ok := s.values[id]
	if !ok || current.Revision != revision {
		return false
	}
	value.Revision = revision + 1
	s.values[id] = cloneChallenge(value)
	return true
}
func (s *MemoryChallengeStore) Consume(id string, revision uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	current, ok := s.values[id]
	if !ok || current.Revision != revision {
		return false
	}
	delete(s.values, id)
	return true
}
func (s *MemoryChallengeStore) Delete(id string) { s.mu.Lock(); delete(s.values, id); s.mu.Unlock() }
func (s *MemoryChallengeStore) prune() {
	now := s.now()
	for id, value := range s.values {
		if !now.Before(value.ExpiresAt) {
			delete(s.values, id)
		}
	}
}
func cloneChallenge(value challenge) challenge {
	value.CodeMAC = append([]byte(nil), value.CodeMAC...)
	return value
}
