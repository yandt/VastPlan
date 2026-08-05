package recordstore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

type recordHost struct{ operations []string }

func (h *recordHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	operation := target.GetOperation()
	h.operations = append(h.operations, operation)
	if _, err := recordstorev1.ParseRequest(operation, payload); err != nil {
		return nil, nil, err
	}
	ok := &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}
	switch operation {
	case recordstorev1.OperationBegin:
		return ok, []byte(`{"transactionHandle":"vptx1.owner.abcdefghijklmnopqrstuvwxyz0123456789","expiresAt":"2030-01-01T00:00:00Z"}`), nil
	case recordstorev1.OperationCreate, recordstorev1.OperationGet, recordstorev1.OperationUpdate:
		return ok, []byte(`{"record":{"id":"item-a"}}`), nil
	case recordstorev1.OperationList:
		return ok, []byte(`{"records":[{"id":"item-a"}]}`), nil
	case recordstorev1.OperationBatch:
		return ok, []byte(`{"results":[]}`), nil
	case recordstorev1.OperationAppendOutbox:
		return ok, []byte(`{"id":"event-a"}`), nil
	default:
		return ok, []byte(`{}`), nil
	}
}

func TestRepositoryCallsTypedRecordStoreOperations(t *testing.T) {
	host := &recordHost{}
	client, _ := New(host)
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "example.plugin"}}
	repository, err := client.Repository(call, recordstorev1.ModelRef{ID: "example.item", SchemaVersion: 1, SHA256: "1111111111111111111111111111111111111111111111111111111111111111"}, recordstorev1.StorageTarget{})
	if err != nil {
		t.Fatal(err)
	}
	created, err := repository.Create(context.Background(), recordstorev1.Record{"id": json.RawMessage(`"item-a"`)}, "create:item-a")
	if err != nil || string(created["id"]) != `"item-a"` {
		t.Fatalf("Create 失败: record=%v err=%v", created, err)
	}
	if _, err := repository.List(context.Background(), nil, nil, 20, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AppendOutbox(context.Background(), "item.created", json.RawMessage(`{"id":"item-a"}`), "outbox:item-a"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(host.operations, []string{recordstorev1.OperationCreate, recordstorev1.OperationList, recordstorev1.OperationAppendOutbox}) {
		t.Fatalf("调用序列错误: %v", host.operations)
	}
}

func TestRepositoryUnitOfWorkCommitsOrRollsBack(t *testing.T) {
	host := &recordHost{}
	client, _ := New(host)
	repository, _ := client.Repository(&contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "example.plugin"}},
		recordstorev1.ModelRef{ID: "example.item", SchemaVersion: 1, SHA256: "1111111111111111111111111111111111111111111111111111111111111111"}, recordstorev1.StorageTarget{})
	options := databasev1.TransactionOptions{Isolation: "serializable", TimeoutMS: 5_000}
	if err := repository.UnitOfWork(context.Background(), options, func(tx *ModelClient) error {
		_, err := tx.Get(context.Background(), recordstorev1.Key{"id": json.RawMessage(`"item-a"`)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.UnitOfWork(context.Background(), options, func(*ModelClient) error { return errors.New("stop") }); err == nil {
		t.Fatal("业务错误必须触发 rollback")
	}
	want := []string{recordstorev1.OperationBegin, recordstorev1.OperationGet, recordstorev1.OperationCommit, recordstorev1.OperationBegin, recordstorev1.OperationRollback}
	if !reflect.DeepEqual(host.operations, want) {
		t.Fatalf("UnitOfWork 调用序列错误: got=%v want=%v", host.operations, want)
	}
}
