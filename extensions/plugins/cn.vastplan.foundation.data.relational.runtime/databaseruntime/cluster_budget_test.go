package databaseruntime

import "testing"

func TestClusterManagerPolicyBoundsEveryReplicaIncludingRollingOverlap(t *testing.T) {
	policy, err := ClusterManagerPolicy(120, 3)
	if err != nil {
		t.Fatal(err)
	}
	if policy.NodeMaxOpen != 40 || policy.TenantMaxOpen != 40 || policy.ConnectionMaxOpen != 40 {
		t.Fatalf("集群预算未按最大副本数切分: %+v", policy)
	}
	if policy.MaxGenerations != 2 || policy.NodeMaxOpen*3 > 120 {
		t.Fatalf("双代轮换或集群硬上限无效: %+v", policy)
	}
}

func TestClusterManagerPolicyRejectsInsufficientRollingBudget(t *testing.T) {
	if _, err := ClusterManagerPolicy(5, 3); err == nil {
		t.Fatal("每副本不足双代轮换的预算必须拒绝")
	}
	if _, err := ClusterManagerPolicy(10, 0); err == nil {
		t.Fatal("缺少最大副本数必须拒绝")
	}
}
