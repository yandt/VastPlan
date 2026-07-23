package servicemodel

import "testing"

func TestNormalizeLegacyDefaultsToActiveActive(t *testing.T) {
	got := Normalize(Policy{})
	want := Policy{InstancePolicy: PolicyActiveActive, StateModel: StateExternalShared, Visibility: VisibilityCluster, Routing: RoutingQueue}
	if got != want {
		t.Fatalf("legacy 默认策略=%+v，期望=%+v", got, want)
	}
}

func TestValidatePerKernel(t *testing.T) {
	if err := Validate(Policy{InstancePolicy: PolicyPerKernel}); err != nil {
		t.Fatalf("per-kernel 默认策略应通过: %v", err)
	}
	if err := Validate(Policy{InstancePolicy: PolicyPerKernel, StateModel: StateExternalShared, Visibility: VisibilityCluster, Routing: RoutingQueue}); err == nil {
		t.Fatal("per-kernel 不得使用集群路由")
	}
}

func TestValidateLeaderAndPartitioned(t *testing.T) {
	for _, policy := range []Policy{
		{InstancePolicy: PolicyLeader},
		{InstancePolicy: PolicyPartitioned},
	} {
		if err := Validate(policy); err != nil {
			t.Fatalf("策略默认组合应通过: %+v: %v", policy, err)
		}
	}
	if err := Validate(Policy{InstancePolicy: PolicyLeader, StateModel: StateExternalShared, Visibility: VisibilityCluster, Routing: RoutingLeader}); err != nil {
		t.Fatalf("单 leader 应允许把可接管账本保存到外部共享状态: %v", err)
	}
	if err := Validate(Policy{InstancePolicy: PolicyLeader, StateModel: StateLocalEphemeral, Visibility: VisibilityCluster, Routing: RoutingLeader}); err == nil {
		t.Fatal("单 leader 不得把持久控制状态声明为 local-ephemeral")
	}
}
