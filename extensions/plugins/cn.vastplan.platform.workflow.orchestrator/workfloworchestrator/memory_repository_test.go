package workfloworchestrator

import (
	"context"
	"sync"
	"time"
)

type memoryRepository struct {
	mu      sync.Mutex
	records map[string]storedRecord
	inTx    bool
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{records: map[string]storedRecord{}}
}

func (r *memoryRepository) Create(_ context.Context, record storedRecord, _ string) (storedRecord, error) {
	if !r.inTx {
		r.mu.Lock()
		defer r.mu.Unlock()
	}
	if _, exists := r.records[record.ID]; exists {
		return storedRecord{}, ErrConflict
	}
	now := time.Now().UTC()
	record.Document = append([]byte(nil), record.Document...)
	record.Revision, record.CreatedAt, record.UpdatedAt = 1, now, now
	r.records[record.ID] = record
	return record, nil
}

func (r *memoryRepository) Get(_ context.Context, id string) (storedRecord, error) {
	if !r.inTx {
		r.mu.Lock()
		defer r.mu.Unlock()
	}
	record, exists := r.records[id]
	if !exists {
		return storedRecord{}, ErrNotFound
	}
	record.Document = append([]byte(nil), record.Document...)
	return record, nil
}

func (r *memoryRepository) List(_ context.Context, kind recordKind, status string) ([]storedRecord, error) {
	if !r.inTx {
		r.mu.Lock()
		defer r.mu.Unlock()
	}
	result := []storedRecord{}
	for _, record := range r.records {
		if record.Kind == kind && (status == "" || record.Status == status) {
			record.Document = append([]byte(nil), record.Document...)
			result = append(result, record)
		}
	}
	return result, nil
}

func (r *memoryRepository) Update(_ context.Context, record storedRecord, expectedRevision int64, _ string) (storedRecord, error) {
	if !r.inTx {
		r.mu.Lock()
		defer r.mu.Unlock()
	}
	current, exists := r.records[record.ID]
	if !exists {
		return storedRecord{}, ErrNotFound
	}
	if current.Revision != expectedRevision {
		return storedRecord{}, ErrConflict
	}
	record.Document = append([]byte(nil), record.Document...)
	record.Revision, record.CreatedAt, record.UpdatedAt = current.Revision+1, current.CreatedAt, time.Now().UTC()
	r.records[record.ID] = record
	return record, nil
}

func (r *memoryRepository) UnitOfWork(_ context.Context, work func(Repository) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyRecords := make(map[string]storedRecord, len(r.records))
	for key, record := range r.records {
		record.Document = append([]byte(nil), record.Document...)
		copyRecords[key] = record
	}
	tx := &memoryRepository{records: copyRecords, inTx: true}
	if err := work(tx); err != nil {
		return err
	}
	r.records = tx.records
	return nil
}

var _ Repository = (*memoryRepository)(nil)
