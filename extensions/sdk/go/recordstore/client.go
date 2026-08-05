// Package recordstore provides the framework-neutral Go client for the
// foundation.data.record-store capability. It carries only model/storage wire
// identities; trusted tenant, service and plugin owner remain host-derived.
package recordstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Client struct{ host sdk.Host }

type ModelClient struct {
	client      *Client
	call        *contractv1.CallContext
	model       recordstorev1.ModelRef
	storage     recordstorev1.StorageTarget
	transaction string
}

type ServiceError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *ServiceError) Error() string {
	if e == nil {
		return "Record Store 调用失败"
	}
	return e.Code + ": " + e.Message
}

func New(host sdk.Host) (*Client, error) {
	if host == nil {
		return nil, errors.New("Record Store client 缺少宿主")
	}
	return &Client{host: host}, nil
}

func (c *Client) Repository(call *contractv1.CallContext, model recordstorev1.ModelRef, storage recordstorev1.StorageTarget) (*ModelClient, error) {
	if c == nil || c.host == nil || call == nil || model.ID == "" || model.SchemaVersion == 0 || model.SHA256 == "" {
		return nil, errors.New("Record Store repository 绑定无效")
	}
	// Keys are model-specific, so validate the common identity and storage
	// target through a shape-complete List request.
	probe, _ := json.Marshal(recordstorev1.ListRequest{Model: model, Storage: storage, Filters: []recordstorev1.Filter{}, Sort: []recordstorev1.Sort{}, Limit: 1})
	if _, err := recordstorev1.ParseRequest(recordstorev1.OperationList, probe); err != nil {
		return nil, fmt.Errorf("Record Store repository 身份无效: %w", err)
	}
	return &ModelClient{client: c, call: call, model: model, storage: storage}, nil
}

func (r *ModelClient) Create(ctx context.Context, record recordstorev1.Record, idempotencyKey string) (recordstorev1.Record, error) {
	request := recordstorev1.CreateRequest{Model: r.model, Storage: r.storage, Record: record, IdempotencyKey: idempotencyKey, TransactionHandle: r.transaction}
	var result recordstorev1.RecordResult
	if err := r.invoke(ctx, recordstorev1.OperationCreate, request, &result); err != nil {
		return nil, err
	}
	return result.Record, nil
}

func (r *ModelClient) Get(ctx context.Context, key recordstorev1.Key) (recordstorev1.Record, error) {
	request := recordstorev1.GetRequest{Model: r.model, Storage: r.storage, Key: key, TransactionHandle: r.transaction}
	var result recordstorev1.RecordResult
	if err := r.invoke(ctx, recordstorev1.OperationGet, request, &result); err != nil {
		return nil, err
	}
	return result.Record, nil
}

func (r *ModelClient) List(ctx context.Context, filters []recordstorev1.Filter, sort []recordstorev1.Sort, limit int, cursor string) (recordstorev1.ListResult, error) {
	if filters == nil {
		filters = []recordstorev1.Filter{}
	}
	if sort == nil {
		sort = []recordstorev1.Sort{}
	}
	request := recordstorev1.ListRequest{Model: r.model, Storage: r.storage, Filters: filters, Sort: sort, Limit: limit, Cursor: cursor, TransactionHandle: r.transaction}
	var result recordstorev1.ListResult
	err := r.invoke(ctx, recordstorev1.OperationList, request, &result)
	return result, err
}

func (r *ModelClient) Update(ctx context.Context, key recordstorev1.Key, values recordstorev1.Record, expectedRevision int64, idempotencyKey string) (recordstorev1.Record, error) {
	request := recordstorev1.UpdateRequest{Model: r.model, Storage: r.storage, Key: key, Values: values, ExpectedRevision: expectedRevision, IdempotencyKey: idempotencyKey, TransactionHandle: r.transaction}
	var result recordstorev1.RecordResult
	if err := r.invoke(ctx, recordstorev1.OperationUpdate, request, &result); err != nil {
		return nil, err
	}
	return result.Record, nil
}

func (r *ModelClient) Delete(ctx context.Context, key recordstorev1.Key, expectedRevision int64, idempotencyKey string) error {
	request := recordstorev1.DeleteRequest{Model: r.model, Storage: r.storage, Key: key, ExpectedRevision: expectedRevision, IdempotencyKey: idempotencyKey, TransactionHandle: r.transaction}
	return r.invoke(ctx, recordstorev1.OperationDelete, request, nil)
}

func (r *ModelClient) Batch(ctx context.Context, mutations []recordstorev1.Mutation) (recordstorev1.BatchResult, error) {
	if mutations == nil {
		mutations = []recordstorev1.Mutation{}
	}
	request := recordstorev1.BatchRequest{Model: r.model, Storage: r.storage, Mutations: mutations, TransactionHandle: r.transaction}
	var result recordstorev1.BatchResult
	err := r.invoke(ctx, recordstorev1.OperationBatch, request, &result)
	return result, err
}

func (r *ModelClient) AppendOutbox(ctx context.Context, topic string, payload json.RawMessage, idempotencyKey string) (recordstorev1.AppendOutboxResult, error) {
	request := recordstorev1.AppendOutboxRequest{Model: r.model, Storage: r.storage, Topic: topic, Payload: payload, IdempotencyKey: idempotencyKey, TransactionHandle: r.transaction}
	var result recordstorev1.AppendOutboxResult
	err := r.invoke(ctx, recordstorev1.OperationAppendOutbox, request, &result)
	return result, err
}

func (r *ModelClient) UnitOfWork(ctx context.Context, options databasev1.TransactionOptions, work func(*ModelClient) error) error {
	if work == nil || r.transaction != "" {
		return errors.New("Record Store UnitOfWork 嵌套或回调无效")
	}
	var begin recordstorev1.BeginResult
	if err := r.invoke(ctx, recordstorev1.OperationBegin, recordstorev1.BeginRequest{Model: r.model, Storage: r.storage, Options: options}, &begin); err != nil {
		return err
	}
	tx := *r
	tx.transaction = begin.TransactionHandle
	if err := work(&tx); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.invoke(rollbackCtx, recordstorev1.OperationRollback, recordstorev1.EndRequest{TransactionHandle: begin.TransactionHandle}, nil)
		return err
	}
	return tx.invoke(ctx, recordstorev1.OperationCommit, recordstorev1.EndRequest{TransactionHandle: begin.TransactionHandle}, nil)
}

func (r *ModelClient) invoke(ctx context.Context, operation string, request any, result any) error {
	if r == nil || r.client == nil || r.client.host == nil || r.call == nil {
		return errors.New("Record Store repository 未绑定")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	parsed, err := recordstorev1.ParseRequest(operation, raw)
	if err != nil {
		return fmt.Errorf("Record Store 请求无效: %w", err)
	}
	raw, err = json.Marshal(parsed)
	if err != nil {
		return err
	}
	response, payload, err := r.client.host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: recordstorev1.Capability, Operation: &operation,
	}, r.call, raw)
	if err != nil {
		return fmt.Errorf("调用 Record Store %s: %w", operation, err)
	}
	if response == nil || response.GetStatus() != contractv1.CallResult_STATUS_OK {
		serviceErr := &ServiceError{Code: recordstorev1.ErrorUnavailable, Message: "Record Store 拒绝调用", Retryable: true}
		if response != nil && response.Error != nil {
			serviceErr.Code, serviceErr.Message, serviceErr.Retryable = response.Error.Code, response.Error.Message, response.Error.Retryable
		}
		return serviceErr
	}
	if result == nil {
		return nil
	}
	if len(payload) == 0 || json.Unmarshal(payload, result) != nil {
		return errors.New("Record Store 返回无效结果")
	}
	return nil
}

func IsCode(err error, code string) bool {
	var serviceErr *ServiceError
	return errors.As(err, &serviceErr) && serviceErr.Code == code
}
