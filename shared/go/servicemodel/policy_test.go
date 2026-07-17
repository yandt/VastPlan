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
}
