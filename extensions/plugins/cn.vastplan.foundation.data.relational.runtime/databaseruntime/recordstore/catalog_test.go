package recordstore

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

const (
	testInventoryDigest = "1111111111111111111111111111111111111111111111111111111111111111"
	testArtifactDigest  = "2222222222222222222222222222222222222222222222222222222222222222"
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
	request := recordstorev1.SyncModelsRequest{Generation: 2, InventoryDigest: testInventoryDigest, Models: []recordstorev1.SignedModel{EncodeSignedModel("cn.vastplan.example.orders", testArtifactDigest, ref, document)}}
	result, err := catalog.Replace(request)
	if err != nil || result.Generation != 2 || result.Models != 1 {
		t.Fatalf("替换模型目录失败: %+v %v", result, err)
	}
	entry, err := catalog.Resolve(ref)
	if err != nil || entry.OwnerPluginID != "cn.vastplan.example.orders" {
		t.Fatalf("解析模型失败: %+v %v", entry, err)
	}
	if _, err := catalog.Replace(recordstorev1.SyncModelsRequest{Generation: 1, InventoryDigest: testInventoryDigest}); err == nil {
		t.Fatal("generation 不得回退")
	}

	tampered := request
	tampered.Models = append([]recordstorev1.SignedModel(nil), request.Models...)
	tampered.Models[0].DocumentBase64 += "AA"
	if _, err := catalog.Replace(tampered); err == nil {
		t.Fatal("同 generation 或摘要漂移必须拒绝")
	}
	missing := ref
	missing.ID = "example.missing"
	if _, err := catalog.Resolve(missing); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("未知模型应返回稳定错误: %v", err)
	}
	mismatch := ref
	mismatch.SchemaVersion++
	if _, err := catalog.Resolve(mismatch); !errors.Is(err, ErrModelMismatch) {
		t.Fatalf("模型身份漂移应返回稳定错误: %v", err)
	}
}

func TestCatalogBindsMigrationToSignedModelAndVersionEdge(t *testing.T) {
	model := testModel()
	model.SchemaVersion = 2
	document, _ := MarshalModel(model)
	modelRef, _ := ModelRef(document)
	fromDigest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	migrationDocument := []byte(fmt.Sprintf(`{"contract":"data.migration.v1","id":"example.order.v2","modelId":"example.order","from":{"schemaVersion":1,"sha256":"%s"},"to":{"schemaVersion":2,"sha256":"%s"},"requiresBackup":true,"requiresApproval":true,"retrySafe":true,"providers":[{"providerId":"postgresql","statements":["ALTER TABLE orders DROP COLUMN legacy"]}]}`, fromDigest, modelRef.SHA256))
	migrationRef := recordstorev1.MigrationRef{ID: "example.order.v2", ModelID: model.ID, FromVersion: 1, ToVersion: 2, SHA256: fmt.Sprintf("%x", sha256Sum(migrationDocument))}
	catalog := NewCatalog()
	result, err := catalog.Replace(recordstorev1.SyncModelsRequest{Generation: 1, InventoryDigest: testInventoryDigest,
		Models:     []recordstorev1.SignedModel{EncodeSignedModel("cn.vastplan.example.orders", testArtifactDigest, modelRef, document)},
		Migrations: []recordstorev1.SignedMigration{EncodeSignedMigration("cn.vastplan.example.orders", testArtifactDigest, migrationRef, migrationDocument)},
	})
	if err != nil || result.Migrations != 1 {
		t.Fatalf("同步迁移失败: %+v %v", result, err)
	}
	entry, _ := catalog.Resolve(modelRef)
	resolved, err := catalog.ResolveMigration(entry, SchemaState{Version: 1, SHA256: fromDigest}, migrationRef.ID)
	if err != nil || resolved.Ref != migrationRef {
		t.Fatalf("解析迁移失败: %+v %v", resolved, err)
	}
	if _, err := catalog.ResolveMigration(entry, SchemaState{Version: 1, SHA256: modelRef.SHA256}, migrationRef.ID); !errors.Is(err, ErrMigrationNeeded) {
		t.Fatalf("来源摘要漂移必须拒绝: %v", err)
	}
}

func sha256Sum(raw []byte) [32]byte { return sha256.Sum256(raw) }

func testModel() datamodelv1.Model {
	return datamodelv1.Model{
		Contract: "data.model.v1", ID: "example.order", SchemaVersion: 1,
		Storage: datamodelv1.StorageBinding{Kind: "connection-ref", Table: "orders"},
		Fields: []datamodelv1.Field{
			{ID: "id", Column: "id", Type: "uuid", Sensitivity: "internal"},
			{ID: "tenantId", Column: "tenant_id", Type: "string", Sensitivity: "confidential", MaxLength: 160},
			{ID: "name", Column: "name", Type: "string", Sensitivity: "internal", MaxLength: 160},
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
