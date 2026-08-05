package databaseruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

type platformRecordTestStore struct {
	modelSHA string
	closed   chan struct{}
}

func (s *platformRecordTestStore) ProviderID() string { return "postgresql" }
func (s *platformRecordTestStore) Read(ctx context.Context, work func(recordstore.Session) error) error {
	return work(s)
}
func (s *platformRecordTestStore) Write(ctx context.Context, work func(recordstore.Session) error) error {
	return work(s)
}
func (s *platformRecordTestStore) Begin(context.Context, databasev1.TransactionOptions) (Transaction, error) {
	return s, nil
}
func (s *platformRecordTestStore) WithPinned(ctx context.Context, work func(PinnedSession) error) error {
	return work(s)
}
func (s *platformRecordTestStore) Closed() <-chan struct{} { return s.closed }
func (s *platformRecordTestStore) Query(_ context.Context, statement databasev1.Statement, _ int) (databasev1.QueryResult, error) {
	if strings.Contains(statement.SQL, "vastplan_schema_migrations") {
		return databasev1.QueryResult{Rows: [][]databasev1.Value{{
			{Type: "int64", Value: json.RawMessage(`"1"`)},
			{Type: "string", Value: mustTestJSON(s.modelSHA)},
		}}}, nil
	}
	if strings.Contains(statement.SQL, `FROM "platform_records"`) {
		return databasev1.QueryResult{Rows: [][]databasev1.Value{{
			{Type: "string", Value: json.RawMessage(`"f119df99-6c60-4e21-9c44-47766593c8e2"`)},
		}}}, nil
	}
	return databasev1.QueryResult{}, nil
}
func (*platformRecordTestStore) Execute(context.Context, databasev1.Statement) (databasev1.ExecuteResult, error) {
	return databasev1.ExecuteResult{RowsAffected: 1}, nil
}
func (*platformRecordTestStore) Commit(context.Context) error   { return nil }
func (*platformRecordTestStore) Rollback(context.Context) error { return nil }

func mustTestJSON(value string) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func TestRecordStoreSynchronizesModelsOnlyFromTrustedSystem(t *testing.T) {
	service, _ := newExecutionService(t)
	document := []byte(`{"contract":"data.model.v1","id":"example.order","schemaVersion":1,"storage":{"kind":"connection-ref","table":"orders"},"fields":[{"id":"id","column":"id","type":"uuid","nullable":false,"sensitivity":"internal"}],"primaryKey":["id"],"indexes":[],"uniqueConstraints":[],"scope":{"tenant":"none","service":"none"},"deletion":{"mode":"hard"}}`)
	ref, err := recordstore.ModelRef(document)
	if err != nil {
		t.Fatal(err)
	}
	inventorDigest := "1111111111111111111111111111111111111111111111111111111111111111"
	artifactDigest := "2222222222222222222222222222222222222222222222222222222222222222"
	request := recordstorev1.SyncModelsRequest{Generation: 1, InventoryDigest: inventorDigest, Models: []recordstorev1.SignedModel{
		recordstore.EncodeSignedModel("cn.vastplan.application.orders", artifactDigest, ref, document),
	}}
	contribution := service.RecordContribution()
	if contribution.ID != recordstorev1.Capability || !json.Valid(contribution.Descriptor) || len(contribution.Handlers) != 15 {
		t.Fatalf("Record Store contribution 无效: %+v", contribution)
	}
	raw, _ := json.Marshal(request)
	denied, _, err := contribution.Handlers[recordstorev1.OperationSyncModels](context.Background(), &runtimeServiceHost{}, executorCall(databaseRef("orders", 1), true), raw)
	if err != nil || denied.GetStatus() != contractv1.CallResult_STATUS_ERROR {
		t.Fatalf("普通插件不得同步 DataModel: %+v %v", denied, err)
	}
	unboundSystem := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "composition-controller"}}
	denied, _, err = contribution.Handlers[recordstorev1.OperationSyncModels](context.Background(), &runtimeServiceHost{}, unboundSystem, raw)
	if err != nil || denied.GetStatus() != contractv1.CallResult_STATUS_ERROR {
		t.Fatalf("缺少 Inventory 证据的 SYSTEM 不得同步目录: %+v %v", denied, err)
	}
	scope := "service"
	system := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "composition-controller"}, Credentials: []*contractv1.CredentialRef{{Name: "plugin.inventory/" + inventorDigest, Scope: &scope}}}
	accepted, payload, err := contribution.Handlers[recordstorev1.OperationSyncModels](context.Background(), &runtimeServiceHost{}, system, raw)
	if err != nil || accepted.GetStatus() != contractv1.CallResult_STATUS_OK || string(payload) != `{"generation":1,"models":1,"migrations":0}` {
		t.Fatalf("可信系统同步 DataModel 失败: %+v %s %v", accepted, payload, err)
	}
}

func TestRecordStoreRejectsCrossPluginModelUseBeforeDatabaseAccess(t *testing.T) {
	service, spec := newExecutionService(t)
	document := []byte(`{"contract":"data.model.v1","id":"example.order","schemaVersion":1,"storage":{"kind":"connection-ref","table":"orders"},"fields":[{"id":"id","column":"id","type":"uuid","nullable":false,"sensitivity":"internal"}],"primaryKey":["id"],"indexes":[],"uniqueConstraints":[],"scope":{"tenant":"none","service":"none"},"deletion":{"mode":"hard"}}`)
	ref, _ := recordstore.ModelRef(document)
	_, _ = service.recordModels.Replace(recordstorev1.SyncModelsRequest{Generation: 1,
		InventoryDigest: "1111111111111111111111111111111111111111111111111111111111111111",
		Models:          []recordstorev1.SignedModel{recordstore.EncodeSignedModel("cn.vastplan.application.orders", "2222222222222222222222222222222222222222222222222222222222222222", ref, document)},
	})
	request := recordstorev1.GetRequest{Storage: recordstorev1.StorageTarget{Connection: &spec.Ref}, Model: ref, Key: recordstorev1.Key{"id": json.RawMessage(`"f119df99-6c60-4e21-9c44-47766593c8e2"`)}}
	call := executorCall(spec.Ref, true)
	call.Caller.Id = "cn.vastplan.application.other"
	result, _ := invokeRecord(t, service, &runtimeServiceHost{}, recordstorev1.OperationGet, call, request)
	if result.GetStatus() != contractv1.CallResult_STATUS_ERROR || result.GetError().GetCode() != recordstorev1.ErrorStorageDenied {
		t.Fatalf("跨插件不得直接读取其他模型: %+v", result)
	}
}

func TestRecordStoreUsesTrustedPlatformBindingWithoutConnectionGrant(t *testing.T) {
	service, ref, _ := newPlatformRecordService(t)
	call := executorCall(databasev1.ConnectionRef{}, false)
	request := recordstorev1.GetRequest{Model: ref, Key: recordstorev1.Key{"id": json.RawMessage(`"f119df99-6c60-4e21-9c44-47766593c8e2"`)}}
	result, raw := invokeRecord(t, service, &runtimeServiceHost{}, recordstorev1.OperationGet, call, request)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("平台控制 Record Store 读取失败: %+v", result)
	}
	var record recordstorev1.RecordResult
	if json.Unmarshal(raw, &record) != nil || string(record.Record["id"]) != `"f119df99-6c60-4e21-9c44-47766593c8e2"` {
		t.Fatalf("平台控制 Record Store 响应无效: %s", raw)
	}
}

func TestPlatformRecordStoreUnitOfWorkUsesInstanceAffineTransaction(t *testing.T) {
	service, ref, _ := newPlatformRecordService(t)
	call := executorCall(databasev1.ConnectionRef{}, false)
	result, raw := invokeRecord(t, service, &runtimeServiceHost{}, recordstorev1.OperationBegin, call,
		recordstorev1.BeginRequest{Model: ref, Options: databasev1.TransactionOptions{Isolation: "serializable", TimeoutMS: 5_000}})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("平台控制 UnitOfWork 开始失败: %+v", result)
	}
	var begin recordstorev1.BeginResult
	if json.Unmarshal(raw, &begin) != nil || begin.TransactionHandle == "" {
		t.Fatalf("平台控制 UnitOfWork 句柄无效: %s", raw)
	}
	get := recordstorev1.GetRequest{Model: ref, Key: recordstorev1.Key{"id": json.RawMessage(`"f119df99-6c60-4e21-9c44-47766593c8e2"`)}, TransactionHandle: begin.TransactionHandle}
	if result, _ = invokeRecord(t, service, &runtimeServiceHost{}, recordstorev1.OperationGet, call, get); result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("平台控制 UnitOfWork 读取失败: %+v", result)
	}
	if result, _ = invokeRecord(t, service, &runtimeServiceHost{}, recordstorev1.OperationCommit, call,
		recordstorev1.EndRequest{TransactionHandle: begin.TransactionHandle}); result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("平台控制 UnitOfWork 提交失败: %+v", result)
	}
}

func newPlatformRecordService(t *testing.T) (*Service, recordstorev1.ModelRef, *platformRecordTestStore) {
	t.Helper()
	service, _ := newExecutionService(t)
	binding := NewPlatformRecordBinding()
	service.platformRecords = binding
	document := []byte(`{"contract":"data.model.v1","id":"example.platform-record","schemaVersion":1,"storage":{"kind":"platform-control","table":"platform_records"},"fields":[{"id":"id","column":"id","type":"uuid","nullable":false,"sensitivity":"internal"}],"primaryKey":["id"],"indexes":[],"uniqueConstraints":[],"scope":{"tenant":"none","service":"none"},"deletion":{"mode":"hard"}}`)
	ref, err := recordstore.ModelRef(document)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.recordModels.Replace(recordstorev1.SyncModelsRequest{Generation: 1,
		InventoryDigest: "1111111111111111111111111111111111111111111111111111111111111111",
		Models: []recordstorev1.SignedModel{recordstore.EncodeSignedModel("cn.vastplan.application.orders",
			"2222222222222222222222222222222222222222222222222222222222222222", ref, document)},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &platformRecordTestStore{modelSHA: ref.SHA256, closed: make(chan struct{})}
	if err := binding.Bind(7, "sha256:platform", store); err != nil {
		t.Fatal(err)
	}
	return service, ref, store
}

func invokeRecord(t *testing.T, service *Service, host *runtimeServiceHost, operation string, call *contractv1.CallContext, request any) (*contractv1.CallResult, []byte) {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	result, payload, err := service.RecordContribution().Handlers[operation](context.Background(), host, call, raw)
	if err != nil {
		t.Fatal(err)
	}
	return result, payload
}

func databaseRef(resourceID string, revision uint64) databasev1.ConnectionRef {
	return databasev1.ConnectionRef{ResourceID: resourceID, Revision: revision}
}
