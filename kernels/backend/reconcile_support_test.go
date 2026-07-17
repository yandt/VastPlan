package main

import (
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/kernels/backend/nodeagent"
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
	if local.thirdPartyPluginPolicy != string(nodeagent.PublisherPolicyRequireIsolation) ||
		local.executionPolicy.PublisherPolicies["vastplan"] != nodeagent.PublisherPolicyAllowTrusted {
		t.Fatalf("默认插件策略必须安全并兼容 vastplan: %+v", local.executionPolicy)
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

func TestParseReconcileOptionsSupportsPublisherOverridesAndLegacyMigration(t *testing.T) {
	configured, err := parseReconcileOptions([]string{
		"-desired", "desired.json",
		"-third-party-plugin-policy", "deny",
		"-publisher-plugin-policies", "partner=allow-trusted,vastplan=require-isolation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.executionPolicy.DefaultPolicy != nodeagent.PublisherPolicyDeny ||
		configured.executionPolicy.PublisherPolicies["partner"] != nodeagent.PublisherPolicyAllowTrusted ||
		configured.executionPolicy.PublisherPolicies["vastplan"] != nodeagent.PublisherPolicyRequireIsolation {
		t.Fatalf("发布者显式规则必须优先于全局和兼容名单: %+v", configured.executionPolicy)
	}

	legacy, err := parseReconcileOptions([]string{
		"-desired", "desired.json", "-require-third-party-isolation=false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.executionPolicy.DefaultPolicy != nodeagent.PublisherPolicyAllowTrusted {
		t.Fatalf("旧布尔参数 false 应迁移为 allow-trusted: %+v", legacy.executionPolicy)
	}
}

func TestParseReconcileOptionsRejectsConflictingOrInvalidPluginPolicies(t *testing.T) {
	tests := [][]string{
		{"-desired", "desired.json", "-third-party-plugin-policy", "deny", "-require-third-party-isolation=false"},
		{"-desired", "desired.json", "-third-party-plugin-policy", "invalid"},
		{"-desired", "desired.json", "-publisher-plugin-policies", "acme=deny,acme=allow-trusted"},
	}
	for _, args := range tests {
		if _, err := parseReconcileOptions(args); err == nil {
			t.Fatalf("冲突/无效策略必须在启动前拒绝: %v", args)
		} else if strings.TrimSpace(err.Error()) == "" {
			t.Fatalf("策略错误必须可诊断: %v", args)
		}
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
