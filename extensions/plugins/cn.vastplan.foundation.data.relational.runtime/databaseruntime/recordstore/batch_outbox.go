package recordstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

func (e *Engine) Batch(ctx context.Context, session Session, compiler *Compiler, entry ModelEntry,
	request recordstorev1.BatchRequest, scope TrustedScope, identity ExecutionIdentity) (recordstorev1.BatchResult, error) {
	if len(request.Mutations) == 0 || len(request.Mutations) > 100 {
		return recordstorev1.BatchResult{}, errors.New("Batch mutation 数量无效")
	}
	result := recordstorev1.BatchResult{Results: make([]recordstorev1.MutationResult, 0, len(request.Mutations))}
	for _, mutation := range request.Mutations {
		switch mutation.Kind {
		case "create":
			created, err := e.Create(ctx, session, compiler, entry, recordstorev1.CreateRequest{Storage: request.Storage, Model: request.Model, Record: mutation.Record, IdempotencyKey: mutation.IdempotencyKey}, scope, identity)
			if err != nil {
				return recordstorev1.BatchResult{}, err
			}
			result.Results = append(result.Results, recordstorev1.MutationResult{Kind: mutation.Kind, Record: created.Record})
		case "update":
			updated, err := e.Update(ctx, session, compiler, entry, recordstorev1.UpdateRequest{Storage: request.Storage, Model: request.Model, Key: mutation.Key, Values: mutation.Values, ExpectedRevision: mutation.ExpectedRevision, IdempotencyKey: mutation.IdempotencyKey}, scope, identity)
			if err != nil {
				return recordstorev1.BatchResult{}, err
			}
			result.Results = append(result.Results, recordstorev1.MutationResult{Kind: mutation.Kind, Record: updated.Record})
		case "delete":
			err := e.Delete(ctx, session, compiler, entry, recordstorev1.DeleteRequest{Storage: request.Storage, Model: request.Model, Key: mutation.Key, ExpectedRevision: mutation.ExpectedRevision, IdempotencyKey: mutation.IdempotencyKey}, scope, identity)
			if err != nil {
				return recordstorev1.BatchResult{}, err
			}
			result.Results = append(result.Results, recordstorev1.MutationResult{Kind: mutation.Kind})
		default:
			return recordstorev1.BatchResult{}, fmt.Errorf("Batch mutation kind 无效: %s", mutation.Kind)
		}
	}
	return result, nil
}

func (e *Engine) AppendOutbox(ctx context.Context, session Session, dialect Dialect, entry ModelEntry,
	request recordstorev1.AppendOutboxRequest, identity ExecutionIdentity) (recordstorev1.AppendOutboxResult, error) {
	if err := ensureSchema(ctx, session, dialect, entry); err != nil {
		return recordstorev1.AppendOutboxResult{}, err
	}
	var result recordstorev1.AppendOutboxResult
	err := withIdempotency(ctx, session, dialect, identity, request.IdempotencyKey, request, &result, func() error {
		id, err := randomUUID()
		if err != nil {
			return err
		}
		result.ID = id
		outboxIdentity := identityDigest(identity.OwnerPluginID, identity.ModelID, identity.TenantID, identity.ServiceID, request.IdempotencyKey)
		columns := []string{"id", "identity_hash", "owner_plugin_id", "model_id", "tenant_id", "service_id", "topic", "payload", "idempotency_key", "created_at"}
		parameters := []databasev1.Value{
			stringValue(id), stringValue(outboxIdentity), stringValue(identity.OwnerPluginID), stringValue(identity.ModelID), stringValue(identity.TenantID),
			stringValue(identity.ServiceID), stringValue(request.Topic), jsonValue(request.Payload), stringValue(request.IdempotencyKey), timestampValue(nowUTC()),
		}
		statement := databasev1.Statement{SQL: fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", dialect.Quote("vastplan_record_outbox"), quoteAll(dialect, columns), placeholders(dialect, len(columns))), Parameters: parameters}
		executed, err := session.Execute(ctx, statement)
		if err != nil {
			return err
		}
		if executed.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
	return result, err
}

func randomUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}
