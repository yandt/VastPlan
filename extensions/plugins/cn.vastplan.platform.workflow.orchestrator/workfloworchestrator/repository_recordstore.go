package workfloworchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.workflow.orchestrator/generated/go"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	recordsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/recordstore"
)

type recordRepository struct{ wire *recordsdk.ModelClient }

func NewRecordRepository(host sdk.Host, call *contractv1.CallContext) (Repository, error) {
	client, err := recordsdk.New(host)
	if err != nil {
		return nil, err
	}
	wire, err := client.Repository(call, recordstorev1.ModelRef{ID: generated.PlatformWorkflowRecordModelID, SchemaVersion: generated.PlatformWorkflowRecordSchemaVersion, SHA256: generated.PlatformWorkflowRecordModelSHA256}, recordstorev1.StorageTarget{})
	if err != nil {
		return nil, err
	}
	return &recordRepository{wire: wire}, nil
}

func (r *recordRepository) Create(ctx context.Context, record storedRecord, idempotencyKey string) (storedRecord, error) {
	created, err := r.wire.Create(ctx, encodeCreate(record), idempotencyKey)
	if err != nil {
		return storedRecord{}, mapRepositoryError(err)
	}
	return decodeStored(created)
}

func (r *recordRepository) Get(ctx context.Context, id string) (storedRecord, error) {
	wire, err := r.wire.Get(ctx, recordstorev1.Key{"id": raw(id)})
	if err != nil {
		return storedRecord{}, mapRepositoryError(err)
	}
	return decodeStored(wire)
}

func (r *recordRepository) List(ctx context.Context, kind recordKind, status string) ([]storedRecord, error) {
	filters := []recordstorev1.Filter{{Field: "kind", Operator: "eq", Value: raw(string(kind))}}
	if status != "" {
		filters = append(filters, recordstorev1.Filter{Field: "status", Operator: "eq", Value: raw(status)})
	}
	result := []storedRecord{}
	cursor := ""
	for {
		page, err := r.wire.List(ctx, filters, []recordstorev1.Sort{{Field: "createdAt", Direction: "asc"}}, 200, cursor)
		if err != nil {
			return nil, mapRepositoryError(err)
		}
		for _, wire := range page.Records {
			record, err := decodeStored(wire)
			if err != nil {
				return nil, err
			}
			result = append(result, record)
		}
		if page.NextCursor == "" {
			return result, nil
		}
		cursor = page.NextCursor
	}
}

func (r *recordRepository) Update(ctx context.Context, record storedRecord, expectedRevision int64, idempotencyKey string) (storedRecord, error) {
	values := recordstorev1.Record{"record": record.Document}
	for key, value := range map[string]string{"kind": string(record.Kind), "serviceId": record.ServiceID, "featureId": record.FeatureID, "status": record.Status} {
		values[key] = raw(value)
	}
	wire, err := r.wire.Update(ctx, recordstorev1.Key{"id": raw(record.ID)}, values, expectedRevision, idempotencyKey)
	if err != nil {
		return storedRecord{}, mapRepositoryError(err)
	}
	return decodeStored(wire)
}

func (r *recordRepository) UnitOfWork(ctx context.Context, work func(Repository) error) error {
	return r.wire.UnitOfWork(ctx, databasev1.TransactionOptions{}, func(tx *recordsdk.ModelClient) error {
		return work(&recordRepository{wire: tx})
	})
}

func encodeCreate(record storedRecord) recordstorev1.Record {
	result := recordstorev1.Record{"id": raw(record.ID), "kind": raw(string(record.Kind)), "record": record.Document}
	for key, value := range map[string]string{"serviceId": record.ServiceID, "featureId": record.FeatureID, "status": record.Status} {
		if value != "" {
			result[key] = raw(value)
		}
	}
	return result
}

func decodeStored(wire recordstorev1.Record) (storedRecord, error) {
	var result storedRecord
	if err := json.Unmarshal(wire["id"], &result.ID); err != nil {
		return storedRecord{}, err
	}
	var kind string
	if err := json.Unmarshal(wire["kind"], &kind); err != nil {
		return storedRecord{}, err
	}
	result.Kind, result.Document = recordKind(kind), append(json.RawMessage(nil), wire["record"]...)
	for key, target := range map[string]*string{"serviceId": &result.ServiceID, "featureId": &result.FeatureID, "status": &result.Status} {
		if value := wire[key]; len(value) > 0 && string(value) != "null" {
			if err := json.Unmarshal(value, target); err != nil {
				return storedRecord{}, err
			}
		}
	}
	if err := decodeTime(wire["createdAt"], &result.CreatedAt); err != nil {
		return storedRecord{}, err
	}
	if err := decodeTime(wire["updatedAt"], &result.UpdatedAt); err != nil || result.UpdatedAt.Before(result.CreatedAt) {
		return storedRecord{}, errors.New("workflow record audit time is invalid")
	}
	var revision string
	if err := json.Unmarshal(wire["revision"], &revision); err != nil {
		return storedRecord{}, err
	}
	parsed, err := strconv.ParseInt(revision, 10, 64)
	if err != nil || parsed < 1 {
		return storedRecord{}, errors.New("workflow record revision is invalid")
	}
	result.Revision = parsed
	return result, nil
}

func decodeTime(value json.RawMessage, target *time.Time) error {
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err == nil {
		*target = parsed
	}
	return err
}

func mapRepositoryError(err error) error {
	switch {
	case recordsdk.IsCode(err, recordstorev1.ErrorNotFound):
		return ErrNotFound
	case recordsdk.IsCode(err, recordstorev1.ErrorConflict), recordsdk.IsCode(err, recordstorev1.ErrorAlreadyExists):
		return ErrConflict
	default:
		return fmt.Errorf("workflow record store: %w", err)
	}
}

func raw(value string) json.RawMessage { data, _ := json.Marshal(value); return data }

var _ Repository = (*recordRepository)(nil)
