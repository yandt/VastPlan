// Package workfloworchestrator implements durable workflow definition,
// binding, instance, task and action orchestration.
package workfloworchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("workflow record not found")
	ErrConflict     = errors.New("workflow record conflict")
	ErrForbidden    = errors.New("workflow operation forbidden")
	ErrInvalidState = errors.New("workflow state is invalid")
)

type recordKind string

const (
	kindFeature      recordKind = "feature"
	kindNodeTemplate recordKind = "node-template"
	kindNodeProvider recordKind = "node-provider"
	kindDefinition   recordKind = "definition"
	kindBinding      recordKind = "binding"
	kindInstance     recordKind = "instance"
	kindTask         recordKind = "task"
	kindAction       recordKind = "action"
)

type storedRecord struct {
	ID        string
	Kind      recordKind
	ServiceID string
	FeatureID string
	Status    string
	Document  json.RawMessage
	Revision  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Repository interface {
	Create(context.Context, storedRecord, string) (storedRecord, error)
	Get(context.Context, string) (storedRecord, error)
	List(context.Context, recordKind, string) ([]storedRecord, error)
	Update(context.Context, storedRecord, int64, string) (storedRecord, error)
	UnitOfWork(context.Context, func(Repository) error) error
}

func encodeDocument(value any) (json.RawMessage, error) { return json.Marshal(value) }

func decodeDocument[T any](record storedRecord) (T, error) {
	var result T
	err := json.Unmarshal(record.Document, &result)
	return result, err
}
