package interaction

import (
	"context"
	"sync"
)

type memoryRepository struct {
	mu      sync.Mutex
	records map[string]storedRecord
}

func newTestWorkflow() (*Workflow, *memoryRepository) {
	repository := &memoryRepository{records: map[string]storedRecord{}}
	return New().Workflow(repository), repository
}

func (r *memoryRepository) Create(_ context.Context, record storedRecord, _ string) (storedRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.records[record.Request.ID]; exists {
		return storedRecord{}, ErrConflict
	}
	record.Revision = 1
	r.records[record.Request.ID] = cloneStored(record)
	return cloneStored(record), nil
}

func (r *memoryRepository) Get(_ context.Context, tenantID, id string) (storedRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, exists := r.records[id]
	if !exists || record.Request.TenantID != tenantID {
		return storedRecord{}, ErrNotFound
	}
	return cloneStored(record), nil
}

func (r *memoryRepository) List(_ context.Context, tenantID string) ([]storedRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []storedRecord{}
	for _, record := range r.records {
		if record.Request.TenantID == tenantID {
			result = append(result, cloneStored(record))
		}
	}
	return result, nil
}

func (r *memoryRepository) Update(_ context.Context, record storedRecord, expectedRevision int64, _ string) (storedRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.records[record.Request.ID]
	if !exists {
		return storedRecord{}, ErrNotFound
	}
	if current.Revision != expectedRevision {
		return storedRecord{}, ErrConflict
	}
	record.Revision = expectedRevision + 1
	r.records[record.Request.ID] = cloneStored(record)
	return cloneStored(record), nil
}

func cloneStored(record storedRecord) storedRecord {
	record.Record = copyRecord(record.Record)
	return record
}

var _ Repository = (*memoryRepository)(nil)
