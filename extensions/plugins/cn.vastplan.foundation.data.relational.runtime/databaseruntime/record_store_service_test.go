package databaseruntime

import (
	"context"
	"encoding/json"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

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
