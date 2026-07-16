package main

import (
	"testing"

	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
)

func TestParseReconcileOptionsNormalizesLocalAndDeploymentModes(t *testing.T) {
	local, err := parseReconcileOptions([]string{"-desired", "desired.json", "-actual-state", "actual.json"})
	if err != nil {
		t.Fatal(err)
	}
	if local.lockPath != "actual.json.lock" || local.nodeID != "local" {
		t.Fatalf("本地默认值未规范化: %+v", local)
	}

	cluster, err := parseReconcileOptions([]string{
		"-nats-url", "nats://127.0.0.1:4222", "-deployment", "api", "-tenant", "acme", "-node-id", "node-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cluster.assignmentKey != controlplane.AssignmentKey("acme", "api", "node-a") {
		t.Fatalf("deployment 应生成当前节点 assignment key: %+v", cluster)
	}
}

func TestParseReconcileOptionsRejectsAmbiguousOrForeignAssignment(t *testing.T) {
	tests := [][]string{
		{"-nats-url", "nats://127.0.0.1:4222", "-deployment", "api", "-assignment-key", "other"},
		{"-nats-url", "nats://127.0.0.1:4222", "-assignment-key", "deployments/acme/api/nodes/node-b", "-node-id", "node-a"},
	}
	for _, args := range tests {
		if _, err := parseReconcileOptions(args); err == nil {
			t.Fatalf("冲突/越权 assignment 必须在连接 NATS 前拒绝: %v", args)
		}
	}
}
