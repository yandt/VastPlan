package controlplane

import (
	"testing"
	"time"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
)

func TestNodeLeaseAcceptsOnlyMatchingBoundedRecoveryReport(t *testing.T) {
	lease := &NodeLease{record: NodeRecord{SchemaVersion: 4, NodeID: "node-a", TenantID: "acme", Deployment: "platform", UpdatedAt: time.Now().UTC()}}
	report := recoveryv1.NodeReport{
		SchemaVersion: recoveryv1.Version, CapsuleID: "platform", RepositoryID: "seed", Generation: 1,
		NodeID: "node-a", Units: map[string]recoveryv1.UnitReport{"repository": {Status: recoveryv1.UnitReady}}, UpdatedAt: time.Now().UTC(),
	}
	if err := lease.UpdateRecovery(report); err != nil {
		t.Fatal(err)
	}
	report.Units["repository"] = recoveryv1.UnitReport{Status: recoveryv1.UnitFailed}
	if lease.record.Recovery.Units["repository"].Status != recoveryv1.UnitReady {
		t.Fatal("Node Lease must deep-clone recovery reports")
	}
	report.NodeID = "node-b"
	if err := lease.UpdateRecovery(report); err == nil {
		t.Fatal("Node Lease must reject a report for another node")
	}
}
