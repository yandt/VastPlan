package repositoryruntime

import "testing"

func TestPublicationPolicyDefaultsAndBounds(t *testing.T) {
	if policy := (PublicationPolicy{}).normalized(); policy.ApprovalTTLHours != 168 || policy.Validate() != nil {
		t.Fatalf("默认审批策略无效: %+v", policy)
	}
	for _, hours := range []int{-1, 721} {
		if err := (PublicationPolicy{ApprovalTTLHours: hours}).Validate(); err == nil {
			t.Fatalf("越界审批有效期 %d 必须拒绝", hours)
		}
	}
}
