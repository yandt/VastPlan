package main

import (
	"testing"

	approvalv1 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v1"
)

func TestApprovalPolicyConfigurationSelectsOneImplementation(t *testing.T) {
	configuration := runtimeConfiguration{ApprovalPolicy: approvalv1.Profile{
		Protocol: approvalv1.Protocol, ID: "foundation.approval.seed-review", Mode: approvalv1.ModeSingleOperatorReview,
		RequireReason: true, RequireDigestAcknowledgement: true,
	}}
	policy, err := approvalPolicyFromConfiguration(configuration)
	if err != nil || policy.Profile().Mode != approvalv1.ModeSingleOperatorReview {
		t.Fatalf("组合根未选择单人复验策略: profile=%+v err=%v", policy.Profile(), err)
	}
	configuration.ApprovalPolicy.RequireReason, configuration.ApprovalPolicy.RequireDigestAcknowledgement = false, false
	if _, err := approvalPolicyFromConfiguration(configuration); err == nil {
		t.Fatal("缺少原因和摘要确认的单人策略必须拒绝")
	}
}
