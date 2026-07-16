package kernelspi

import (
	"context"
	"errors"
	"sync"
)

var ErrTransactionConflict = errors.New("persistence transaction revision conflict")

// MemoryPersistence 是嵌入式/测试实现，同时给适配器提供事务语义参考。
type MemoryPersistence struct {
	mu       sync.RWMutex
	revision uint64
	values   map[Scope]map[string][]byte
}

func NewMemoryPersistence() *MemoryPersistence {
	return &MemoryPersistence{values: map[Scope]map[string][]byte{}}
}

func (m *MemoryPersistence) Get(_ context.Context, scope Scope, key string) ([]byte, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, errors.New("persistence key 不能为空")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.values[scope][key]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), value...), nil
}
func (m *MemoryPersistence) Put(_ context.Context, scope Scope, key string, value []byte) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if key == "" {
		return errors.New("persistence key 不能为空")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values[scope] == nil {
		m.values[scope] = map[string][]byte{}
	}
	m.values[scope][key] = append([]byte(nil), value...)
	m.revision++
	return nil
}
func (m *MemoryPersistence) Delete(_ context.Context, scope Scope, key string) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if key == "" {
		return errors.New("persistence key 不能为空")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.values[scope][key]; !ok {
		return ErrNotFound
	}
	delete(m.values[scope], key)
	m.revision++
	return nil
}
func (m *MemoryPersistence) Begin(_ context.Context, scope Scope) (Transaction, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	working := map[string][]byte{}
	for key, value := range m.values[scope] {
		working[key] = append([]byte(nil), value...)
	}
	return &memoryTransaction{owner: m, scope: scope, baseRevision: m.revision, working: working}, nil
}

type memoryTransaction struct {
	mu           sync.Mutex
	owner        *MemoryPersistence
	scope        Scope
	baseRevision uint64
	working      map[string][]byte
	closed       bool
}

func (t *memoryTransaction) check(scope Scope) error {
	if t.closed {
		return errors.New("transaction 已关闭")
	}
	if scope != t.scope {
		return errors.New("transaction scope 不匹配")
	}
	return scope.Validate()
}
func (t *memoryTransaction) Get(_ context.Context, scope Scope, key string) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.check(scope); err != nil {
		return nil, err
	}
	value, ok := t.working[key]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), value...), nil
}
func (t *memoryTransaction) Put(_ context.Context, scope Scope, key string, value []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.check(scope); err != nil {
		return err
	}
	if key == "" {
		return errors.New("persistence key 不能为空")
	}
	t.working[key] = append([]byte(nil), value...)
	return nil
}
func (t *memoryTransaction) Delete(_ context.Context, scope Scope, key string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := t.check(scope); err != nil {
		return err
	}
	if _, ok := t.working[key]; !ok {
		return ErrNotFound
	}
	delete(t.working, key)
	return nil
}
func (t *memoryTransaction) Commit(_ context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("transaction 已关闭")
	}
	t.owner.mu.Lock()
	defer t.owner.mu.Unlock()
	if t.owner.revision != t.baseRevision {
		t.closed = true
		return ErrTransactionConflict
	}
	replacement := map[string][]byte{}
	for key, value := range t.working {
		replacement[key] = append([]byte(nil), value...)
	}
	t.owner.values[t.scope] = replacement
	t.owner.revision++
	t.closed = true
	return nil
}
func (t *memoryTransaction) Rollback(_ context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errors.New("transaction 已关闭")
	}
	t.closed = true
	t.working = nil
	return nil
}
