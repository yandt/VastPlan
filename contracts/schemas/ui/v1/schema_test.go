package uiv1

import (
	"testing"
	"time"
)

func validForm() FormSchema {
	return FormSchema{ID: "approve-deployment", Schema: JSONSchema{
		"$schema": JSONSchemaDialect,
		"type":    "object",
		"properties": map[string]any{
			"approved": map[string]any{"type": "boolean", "title": "批准"},
		},
	}}
}

func TestValidateFormSchema(t *testing.T) {
	if err := ValidateFormSchema(validForm()); err != nil {
		t.Fatalf("有效表单被拒绝: %v", err)
	}
	invalid := validForm()
	invalid.Schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	if err := ValidateFormSchema(invalid); err == nil {
		t.Fatal("不支持的 JSON Schema 方言必须拒绝")
	}
	remoteRef := validForm()
	remoteRef.Schema["properties"] = map[string]any{"x": map[string]any{"$ref": "https://attacker.invalid/schema.json"}}
	if err := ValidateFormSchema(remoteRef); err == nil {
		t.Fatal("远程 $ref 必须拒绝")
	}
}

func TestValidateFormData(t *testing.T) {
	form := validForm()
	form.Schema["required"] = []any{"approved"}
	if err := ValidateFormData(form, map[string]any{"approved": true}); err != nil {
		t.Fatalf("有效表单数据被拒绝: %v", err)
	}
	if err := ValidateFormData(form, map[string]any{}); err == nil {
		t.Fatal("缺少 required 字段必须拒绝")
	}
}

func TestValidateInteractionRequest(t *testing.T) {
	request := InteractionRequest{
		ID: "interaction_20260718_0001", ContractVersion: InteractionContractVersion, Kind: InteractionForm,
		Source: InteractionSource{Capability: "platform.runner.deploy", WorkflowRunID: "run-1"}, TenantID: "acme",
		EligibleSubjects: []string{"user:alice"}, AllowedSurfaces: []InteractionSurface{SurfaceFrontend, SurfaceMobile},
		ExpiresAt: time.Now().Add(time.Minute), Form: formPointer(validForm()),
	}
	if err := ValidateInteractionRequest(request); err != nil {
		t.Fatalf("有效交互请求被拒绝: %v", err)
	}
	request.Form = nil
	if err := ValidateInteractionRequest(request); err == nil {
		t.Fatal("form 交互缺少表单必须拒绝")
	}
}

func formPointer(value FormSchema) *FormSchema { return &value }
