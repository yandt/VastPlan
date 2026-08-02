package main

import (
	"testing"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
)

func TestApprovalPolicyConfigurationSelectsOneImplementation(t *testing.T) {
	configuration := runtimeConfiguration{ApprovalPolicy: approvalv2.ProviderBinding{
		Protocol: approvalv2.Protocol, Capability: approvalv2.Capability, LogicalService: "foundation.approval-policy.native", RoutingDomain: "security",
		Profile: approvalv2.ProfileRef{ID: "seed.portal-publication", Revision: 1, Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}}
	binding, err := approvalPolicyFromConfiguration(configuration)
	if err != nil || binding.Profile.ID != "seed.portal-publication" {
		t.Fatalf("组合根未选择精确 Provider Binding: binding=%+v err=%v", binding, err)
	}
	configuration.ApprovalPolicy.Profile.Digest = ""
	if _, err := approvalPolicyFromConfiguration(configuration); err == nil {
		t.Fatal("缺少 Profile digest 的 Provider Binding 必须拒绝")
	}
}
