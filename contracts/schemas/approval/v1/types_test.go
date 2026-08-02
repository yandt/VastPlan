package approvalv1

import "testing"

func TestValidateProfileRequiresExplicitSafeSingleOperatorReview(t *testing.T) {
	strict := Profile{Protocol: Protocol, ID: "foundation.approval.strict", Mode: ModeDifferentSubject}
	if err := ValidateProfile(strict); err != nil {
		t.Fatal(err)
	}
	unsafe := Profile{Protocol: Protocol, ID: "foundation.approval.seed-review", Mode: ModeSingleOperatorReview}
	if err := ValidateProfile(unsafe); err == nil {
		t.Fatal("单人复验不得省略原因和摘要确认")
	}
	safe := unsafe
	safe.RequireReason, safe.RequireDigestAcknowledgement = true, true
	if err := ValidateProfile(safe); err != nil {
		t.Fatal(err)
	}
}
