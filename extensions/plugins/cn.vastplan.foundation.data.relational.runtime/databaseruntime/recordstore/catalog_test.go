package recordstore

import (
	"encoding/json"
	"testing"

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

func TestCatalogAtomicallyReplacesSignedModels(t *testing.T) {
	document, err := MarshalModel(testModel())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := ModelRef(document)
	if err != nil {
		t.Fatal(err)
	}
	catalog := NewCatalog()
	request := recordstorev1.SyncModelsRequest{Generation: 2, Models: []recordstorev1.SignedModel{EncodeSignedModel("cn.vastplan.example.orders", ref, document)}}
	result, err := catalog.Replace(request)
	if err != nil || result.Generation != 2 || result.Models != 1 {
		t.Fatalf("替换模型目录失败: %+v %v", result, err)
	}
	entry, err := catalog.Resolve(ref)
	if err != nil || entry.OwnerPluginID != "cn.vastplan.example.orders" {
		t.Fatalf("解析模型失败: %+v %v", entry, err)
	}
	if _, err := catalog.Replace(recordstorev1.SyncModelsRequest{Generation: 1}); err == nil {
		t.Fatal("generation 不得回退")
	}

	tampered := request
	tampered.Models = append([]recordstorev1.SignedModel(nil), request.Models...)
	tampered.Models[0].DocumentBase64 += "AA"
	if _, err := catalog.Replace(tampered); err == nil {
		t.Fatal("同 generation 或摘要漂移必须拒绝")
	}
}

func testModel() datamodelv1.Model {
	return datamodelv1.Model{
		Contract: "data.model.v1", ID: "example.order", SchemaVersion: 1,
		Storage: datamodelv1.StorageBinding{Kind: "connection-ref", Table: "orders"},
		Fields: []datamodelv1.Field{
			{ID: "id", Column: "id", Type: "uuid", Sensitivity: "internal"},
			{ID: "tenantId", Column: "tenant_id", Type: "string", Sensitivity: "confidential"},
			{ID: "name", Column: "name", Type: "string", Sensitivity: "internal"},
			{ID: "revision", Column: "revision", Type: "int64", Sensitivity: "internal"},
			{ID: "createdAt", Column: "created_at", Type: "timestamp", Sensitivity: "internal"},
			{ID: "updatedAt", Column: "updated_at", Type: "timestamp", Sensitivity: "internal"},
			{ID: "deletedAt", Column: "deleted_at", Type: "timestamp", Nullable: true, Sensitivity: "internal"},
		},
		PrimaryKey: []string{"id"}, Indexes: []datamodelv1.Index{{ID: "byName", Fields: []string{"name"}}},
		UniqueConstraints: []datamodelv1.UniqueConstraint{{ID: "tenantName", Fields: []string{"tenantId", "name"}}},
		Scope:             datamodelv1.Scope{Tenant: "required", Service: "none"},
		OptimisticLock:    &datamodelv1.OptimisticLock{Field: "revision"},
		Audit:             &datamodelv1.AuditFields{CreatedAt: "createdAt", UpdatedAt: "updatedAt"},
		Deletion:          datamodelv1.DeletionPolicy{Mode: "soft", Field: "deletedAt"},
	}
}

func raw(value any) json.RawMessage { encoded, _ := json.Marshal(value); return encoded }
