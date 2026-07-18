package uiv1

import (
	"testing"
	"time"
)

func validForm() FormSchema {
	return FormSchema{ID: "approve-deployment", Fields: []FormField{{Key: "approved", Type: FieldBoolean, Title: "批准"}}}
}

func TestValidateFormSchema(t *testing.T) {
	if err := ValidateFormSchema(validForm()); err != nil {
		t.Fatalf("有效表单被拒绝: %v", err)
	}
	invalid := validForm()
	invalid.Fields[0].Key = "invalid key"
	if err := ValidateFormSchema(invalid); err == nil {
		t.Fatal("非法字段 key 必须拒绝")
	}
}

func TestValidateInteractionRequest(t *testing.T) {
	request := InteractionRequest{
		ID: "interaction_20260718_0001", ContractVersion: InteractionContractVersion, Kind: InteractionForm,
		Source: InteractionSource{Capability: "platform.runner.deploy", WorkflowRunID: "run-1"}, TenantID: "acme",
		EligibleSubjects: []string{"user:alice"}, AllowedSurfaces: []InteractionSurface{SurfaceFrontend, SurfaceMobile},
		ExpiresAt: time.Now().Add(time.Minute), Form: &FormSchema{ID: "approval", Fields: []FormField{{Key: "approved", Type: FieldBoolean, Title: "批准"}}},
	}
	if err := ValidateInteractionRequest(request); err != nil {
		t.Fatalf("有效交互请求被拒绝: %v", err)
	}
	request.Form = nil
	if err := ValidateInteractionRequest(request); err == nil {
		t.Fatal("form 交互缺少表单必须拒绝")
	}
}
