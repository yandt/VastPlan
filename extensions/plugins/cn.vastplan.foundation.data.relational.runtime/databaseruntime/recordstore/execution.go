package recordstore

import (
	"context"
	"encoding/json"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

type Session interface {
	Query(context.Context, databasev1.Statement, int) (databasev1.QueryResult, error)
	Execute(context.Context, databasev1.Statement) (databasev1.ExecuteResult, error)
}

type ExecutionIdentity struct {
	OwnerPluginID string
	ModelID       string
	TenantID      string
	ServiceID     string
	CallerID      string
}

type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

func (e *Engine) CheckSchema(ctx context.Context, session Session, dialect Dialect, entry ModelEntry) error {
	return ensureSchema(ctx, session, dialect, entry)
}

func (e *Engine) Create(ctx context.Context, session Session, compiler *Compiler, entry ModelEntry,
	request recordstorev1.CreateRequest, scope TrustedScope, identity ExecutionIdentity) (recordstorev1.RecordResult, error) {
	if err := ensureSchema(ctx, session, compiler.dialect, entry); err != nil {
		return recordstorev1.RecordResult{}, err
	}
	var result recordstorev1.RecordResult
	err := withIdempotency(ctx, session, compiler.dialect, identity, request.IdempotencyKey, request, &result, func() error {
		statement, prepared, err := compiler.Create(request.Record, scope, nowUTC())
		if err != nil {
			return err
		}
		executed, err := session.Execute(ctx, statement)
		if err != nil {
			if constraintViolation(err) {
				return ErrAlreadyExists
			}
			return err
		}
		if executed.RowsAffected != 1 {
			return ErrConflict
		}
		result.Record = prepared
		return nil
	})
	return result, err
}

func (e *Engine) Get(ctx context.Context, session Session, compiler *Compiler, entry ModelEntry,
	request recordstorev1.GetRequest, scope TrustedScope) (recordstorev1.RecordResult, error) {
	if err := ensureSchema(ctx, session, compiler.dialect, entry); err != nil {
		return recordstorev1.RecordResult{}, err
	}
	statement, err := compiler.Get(request.Key, scope)
	if err != nil {
		return recordstorev1.RecordResult{}, err
	}
	return queryOne(ctx, session, compiler, statement)
}

func (e *Engine) List(ctx context.Context, session Session, compiler *Compiler, entry ModelEntry,
	request recordstorev1.ListRequest, scope TrustedScope) (recordstorev1.ListResult, error) {
	if err := ensureSchema(ctx, session, compiler.dialect, entry); err != nil {
		return recordstorev1.ListResult{}, err
	}
	statement, offset, err := compiler.List(request, scope)
	if err != nil {
		return recordstorev1.ListResult{}, err
	}
	queried, err := session.Query(ctx, statement, request.Limit+1)
	if err != nil {
		return recordstorev1.ListResult{}, err
	}
	result := recordstorev1.ListResult{Records: make([]recordstorev1.Record, 0, min(len(queried.Rows), request.Limit))}
	for index, row := range queried.Rows {
		if index == request.Limit {
			result.NextCursor = EncodeCursor(request.Model, offset+request.Limit)
			break
		}
		record, decodeErr := compiler.DecodeRow(row)
		if decodeErr != nil {
			return recordstorev1.ListResult{}, decodeErr
		}
		result.Records = append(result.Records, record)
	}
	return result, nil
}

func (e *Engine) Update(ctx context.Context, session Session, compiler *Compiler, entry ModelEntry,
	request recordstorev1.UpdateRequest, scope TrustedScope, identity ExecutionIdentity) (recordstorev1.RecordResult, error) {
	if err := ensureSchema(ctx, session, compiler.dialect, entry); err != nil {
		return recordstorev1.RecordResult{}, err
	}
	var result recordstorev1.RecordResult
	err := withIdempotency(ctx, session, compiler.dialect, identity, request.IdempotencyKey, request, &result, func() error {
		statement, err := compiler.Update(request, scope, nowUTC())
		if err != nil {
			return err
		}
		executed, err := session.Execute(ctx, statement)
		if err != nil {
			return err
		}
		if executed.RowsAffected != 1 {
			return ErrConflict
		}
		get, err := compiler.Get(request.Key, scope)
		if err != nil {
			return err
		}
		result, err = queryOne(ctx, session, compiler, get)
		return err
	})
	return result, err
}

func (e *Engine) Delete(ctx context.Context, session Session, compiler *Compiler, entry ModelEntry,
	request recordstorev1.DeleteRequest, scope TrustedScope, identity ExecutionIdentity) error {
	if err := ensureSchema(ctx, session, compiler.dialect, entry); err != nil {
		return err
	}
	var response struct {
		Deleted bool `json:"deleted"`
	}
	return withIdempotency(ctx, session, compiler.dialect, identity, request.IdempotencyKey, request, &response, func() error {
		statement, err := compiler.Delete(request, scope, nowUTC())
		if err != nil {
			return err
		}
		executed, err := session.Execute(ctx, statement)
		if err != nil {
			return err
		}
		if executed.RowsAffected != 1 {
			return ErrConflict
		}
		response.Deleted = true
		return nil
	})
}

func queryOne(ctx context.Context, session Session, compiler *Compiler, statement databasev1.Statement) (recordstorev1.RecordResult, error) {
	queried, err := session.Query(ctx, statement, 2)
	if err != nil {
		return recordstorev1.RecordResult{}, err
	}
	if len(queried.Rows) == 0 {
		return recordstorev1.RecordResult{}, ErrNotFound
	}
	if len(queried.Rows) != 1 {
		return recordstorev1.RecordResult{}, ErrConflict
	}
	record, err := compiler.DecodeRow(queried.Rows[0])
	return recordstorev1.RecordResult{Record: record}, err
}

func clonePayload(value json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), value...)
}
