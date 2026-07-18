package callcontext

import (
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

func TestValidateIngressRejectsConflictingTenantAndReservedMetadata(t *testing.T) {
	_, err := ValidateIngress(&contractv1.CallContext{TenantId: "a", Principal: &contractv1.Principal{TenantId: "b"}}, Provenance{})
	if err == nil {
		t.Fatal("tenant 冲突应被拒绝")
	}
	_, err = ValidateIngress(&contractv1.CallContext{Metadata: map[string]string{"vastplan.internal.token": "x"}}, Provenance{})
	if err == nil {
		t.Fatal("宿主保留 metadata 应被拒绝")
	}
}

func TestProjectionDisclosesOnlyAuthorizedFieldsAndBaggage(t *testing.T) {
	trusted, err := ValidateIngress(&contractv1.CallContext{
		TenantId: "tenant-a", ProjectId: ptr("project-a"), Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "caller"},
		Principal:   &contractv1.Principal{UserId: "u1", Username: "name", IsAdmin: true, SystemRoles: []string{"admin"}},
		Credentials: []*contractv1.CredentialRef{{Name: "db"}}, Metadata: map[string]string{"com.example.visible": "yes", "com.other.hidden": "no"},
	}, Provenance{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := trusted.Project(Projection{Fields: MustAccess(FieldScopeTenant, FieldSubjectID, FieldBaggage), BaggagePrefixes: []string{"com.example.*"}})
	if err != nil {
		t.Fatal(err)
	}
	if projected.TenantId != "tenant-a" || projected.GetPrincipal().GetUserId() != "u1" {
		t.Fatalf("投影缺失授权字段: %+v", projected)
	}
	if projected.GetProjectId() != "" || projected.GetPrincipal().GetUsername() != "" || projected.GetPrincipal().GetIsAdmin() || len(projected.Credentials) != 0 {
		t.Fatalf("投影泄露未授权字段: %+v", projected)
	}
	if len(projected.Metadata) != 1 || projected.Metadata["com.example.visible"] != "yes" {
		t.Fatalf("baggage 前缀投影错误: %v", projected.Metadata)
	}
	projected.Principal.UserId = "mutated"
	if trusted.Views().Subject.ID() != "u1" {
		t.Fatal("投影修改污染可信基线")
	}
}

func TestEffectiveProjectionFailsClosedForMissingRequiredField(t *testing.T) {
	_, err := EffectiveProjection(MustAccess(FieldSubjectID), MustAccess(FieldTrace), nil, MustAccess(FieldTrace))
	if err == nil {
		t.Fatal("必需字段未通过上限时应拒绝")
	}
}

func ptr(value string) *string { return &value }
