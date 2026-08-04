package datamodelgen

import (
	"bytes"
	"strings"
	"testing"

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
)

func TestGenerateIsDeterministicAndComplete(t *testing.T) {
	model := testModel()
	for _, language := range SupportedLanguages() {
		t.Run(string(language), func(t *testing.T) {
			first, err := Generate(model, language, "generated")
			if err != nil {
				t.Fatal(err)
			}
			second, err := Generate(model, language, "generated")
			if err != nil {
				t.Fatal(err)
			}
			if first.Filename != second.Filename || !bytes.Equal(first.Content, second.Content) {
				t.Fatal("同一 DataModel 的生成结果必须确定")
			}
			for _, required := range []string{"create", "get", "list", "update", "delete", "batch", "outbox"} {
				if !strings.Contains(strings.ToLower(string(first.Content)), required) {
					t.Fatalf("%s 生成物缺少 Repository 能力 %s", language, required)
				}
			}
			if !strings.Contains(strings.ToLower(string(first.Content)), "unitofwork") &&
				!strings.Contains(strings.ToLower(string(first.Content)), "unit_of_work") {
				t.Fatalf("%s 生成物缺少 UnitOfWork", language)
			}
		})
	}
}

func TestGenerateRejectsInvalidInput(t *testing.T) {
	model := testModel()
	model.PrimaryKey = []string{"missing"}
	if _, err := Generate(model, Go, "generated"); err == nil {
		t.Fatal("生成器不得绕过 DataModel 语义校验")
	}
	if _, err := Generate(testModel(), "java", "generated"); err == nil {
		t.Fatal("未实现的语言必须拒绝")
	}
	if _, err := Generate(testModel(), Go, ""); err == nil {
		t.Fatal("空包名必须拒绝")
	}
	if _, err := Generate(testModel(), Go, "bad-package"); err == nil {
		t.Fatal("非法 Go 包名必须拒绝")
	}
}

func testModel() datamodelv1.Model {
	return datamodelv1.Model{
		Contract:      "data.model.v1",
		ID:            "example.record",
		SchemaVersion: 1,
		Storage:       datamodelv1.StorageBinding{Kind: "connection-ref", Table: "example_records"},
		Fields: []datamodelv1.Field{
			{ID: "id", Column: "id", Type: "uuid", Sensitivity: "internal"},
			{ID: "revision", Column: "revision", Type: "int64", Sensitivity: "internal"},
			{ID: "createdAt", Column: "created_at", Type: "timestamp", Sensitivity: "internal"},
			{ID: "updatedAt", Column: "updated_at", Type: "timestamp", Sensitivity: "internal"},
			{ID: "payload", Column: "payload", Type: "json", Nullable: true, Sensitivity: "confidential"},
		},
		PrimaryKey:        []string{"id"},
		Scope:             datamodelv1.Scope{Tenant: "none", Service: "none"},
		OptimisticLock:    &datamodelv1.OptimisticLock{Field: "revision"},
		Audit:             &datamodelv1.AuditFields{CreatedAt: "createdAt", UpdatedAt: "updatedAt"},
		Deletion:          datamodelv1.DeletionPolicy{Mode: "hard"},
		Indexes:           []datamodelv1.Index{},
		UniqueConstraints: []datamodelv1.UniqueConstraint{},
	}
}
