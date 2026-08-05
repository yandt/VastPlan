package interaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/interactionapi"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.interaction.broker/generated/go"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	recordsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/recordstore"
)

type recordRepository struct{ wire *recordsdk.ModelClient }

func NewRecordRepository(host sdk.Host, call *contractv1.CallContext) (Repository, error) {
	client, err := recordsdk.New(host)
	if err != nil {
		return nil, err
	}
	wire, err := client.Repository(call, recordstorev1.ModelRef{
		ID: generated.PlatformInteractionRecordModelID, SchemaVersion: generated.PlatformInteractionRecordSchemaVersion,
		SHA256: generated.PlatformInteractionRecordModelSHA256,
	}, recordstorev1.StorageTarget{})
	if err != nil {
		return nil, err
	}
	return &recordRepository{wire: wire}, nil
}

func (r *recordRepository) Create(ctx context.Context, record storedRecord, idempotencyKey string) (storedRecord, error) {
	wire, err := encodeCreate(record)
	if err != nil {
		return storedRecord{}, err
	}
	created, err := r.wire.Create(ctx, wire, idempotencyKey)
	if err != nil {
		return storedRecord{}, mapRepositoryError(err)
	}
	return decodeStored(created)
}

func (r *recordRepository) Get(ctx context.Context, _ string, id string) (storedRecord, error) {
	wire, err := r.wire.Get(ctx, recordstorev1.Key{"id": mustRaw(id)})
	if err != nil {
		return storedRecord{}, mapRepositoryError(err)
	}
	return decodeStored(wire)
}

func (r *recordRepository) List(ctx context.Context, _ string) ([]storedRecord, error) {
	result := []storedRecord{}
	cursor := ""
	for {
		page, err := r.wire.List(ctx, nil, []recordstorev1.Sort{{Field: "createdAt", Direction: "asc"}}, 200, cursor)
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
	document, err := json.Marshal(record.Record)
	if err != nil {
		return storedRecord{}, err
	}
	values := recordstorev1.Record{"state": mustRaw(string(record.State)), "record": document}
	wire, err := r.wire.Update(ctx, recordstorev1.Key{"id": mustRaw(record.Request.ID)}, values, expectedRevision, idempotencyKey)
	if err != nil {
		return storedRecord{}, mapRepositoryError(err)
	}
	return decodeStored(wire)
}

func encodeCreate(record storedRecord) (recordstorev1.Record, error) {
	document, err := json.Marshal(record.Record)
	if err != nil {
		return nil, err
	}
	return recordstorev1.Record{
		"id": mustRaw(record.Request.ID), "state": mustRaw(string(record.State)), "requestHash": mustRaw(record.RequestHash),
		"record": document, "expiresAt": mustRaw(record.Request.ExpiresAt.UTC().Format(time.RFC3339Nano)),
	}, nil
}

func decodeStored(wire recordstorev1.Record) (storedRecord, error) {
	var record storedRecord
	if err := json.Unmarshal(wire["record"], &record.Record); err != nil {
		return storedRecord{}, fmt.Errorf("解码交互记录: %w", err)
	}
	var id, tenantID, state, expiresAt, createdAt, updatedAt string
	for field, target := range map[string]*string{
		"id": &id, "tenantId": &tenantID, "state": &state, "requestHash": &record.RequestHash,
		"expiresAt": &expiresAt, "createdAt": &createdAt, "updatedAt": &updatedAt,
	} {
		if err := json.Unmarshal(wire[field], target); err != nil {
			return storedRecord{}, fmt.Errorf("解码交互字段 %s: %w", field, err)
		}
	}
	if id != record.Request.ID || tenantID != record.Request.TenantID || state == "" {
		return storedRecord{}, errors.New("交互记录身份或状态不一致")
	}
	expiry, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil || !expiry.Equal(record.Request.ExpiresAt) {
		return storedRecord{}, errors.New("交互记录过期时间不一致")
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return storedRecord{}, err
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil || updated.Before(created) {
		return storedRecord{}, errors.New("交互记录审计时间无效")
	}
	record.State = interactionapi.State(state)
	record.CreatedAt, record.UpdatedAt = created, updated
	var revision string
	if err := json.Unmarshal(wire["revision"], &revision); err != nil {
		return storedRecord{}, err
	}
	parsed, err := strconv.ParseInt(revision, 10, 64)
	if err != nil || parsed < 1 {
		return storedRecord{}, errors.New("交互记录 revision 无效")
	}
	record.Revision = parsed
	return record, nil
}

func mapRepositoryError(err error) error {
	switch {
	case recordsdk.IsCode(err, recordstorev1.ErrorNotFound):
		return ErrNotFound
	case recordsdk.IsCode(err, recordstorev1.ErrorConflict), recordsdk.IsCode(err, recordstorev1.ErrorAlreadyExists):
		return ErrConflict
	default:
		return err
	}
}

func mustRaw(value string) json.RawMessage { raw, _ := json.Marshal(value); return raw }

var _ Repository = (*recordRepository)(nil)
