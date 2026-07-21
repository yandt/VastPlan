package main

import (
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
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
	if local.placementPolicy.Default != nodeagent.PlacementProcessOnly {
		t.Fatalf("默认必须保持进程隔离: %+v", local.placementPolicy)
	}
	if local.hostingPolicy.Default != nodeagent.RuntimeHostingShared {
		t.Fatalf("托管语言默认应共享兼容 Runtime Host: %+v", local.hostingPolicy)
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

	legacy, err := parseReconcileOptions([]string{"-nats-url", "nats://127.0.0.1:4222", "-desired-key", controlplane.DesiredKey("acme", "legacy-config")})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.assignmentKey != "" || legacy.deploymentName != "" {
		t.Fatalf("仅指定 desired-key 的 NATS Agent 必须保留 desired source 语义: %+v", legacy)
	}
	if tenant, deployment := controlPlaneScope(legacy.deploymentTenant, legacy.deploymentName); tenant != "_global" || deployment != "legacy" {
		t.Fatalf("未声明部署的控制面实际态作用域错误: tenant=%s deployment=%s", tenant, deployment)
	}
}

func TestParseReconcileOptionsSupportsYAMLStartupFileAlias(t *testing.T) {
	configured, err := parseReconcileOptions([]string{"-startup-file", "desired.yaml"})
	if err != nil || configured.desiredPath != "desired.yaml" {
		t.Fatalf("启动配置别名未解析: %+v %v", configured, err)
	}
	if _, err := parseReconcileOptions([]string{"-desired", "desired.json", "-startup-file", "desired.yaml"}); err == nil {
		t.Fatal("两个配置入口同时设置必须拒绝")
	}
}

func TestParseReconcileOptionsSupportsPlacementPrecedence(t *testing.T) {
	configured, err := parseReconcileOptions([]string{
		"-desired", "desired.json",
		"-plugin-placement-default", "process-only",
		"-publisher-plugin-placements", "vastplan=prefer-dynamic-go",
		"-plugin-placements", "cn.vastplan.foundation.security.bootstrap-policy=require-dynamic-go",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.placementPolicy.PublisherPolicies["vastplan"] != nodeagent.PlacementPreferDynamicGo ||
		configured.placementPolicy.PluginPolicies["cn.vastplan.foundation.security.bootstrap-policy"] != nodeagent.PlacementRequireDynamicGo {
		t.Fatalf("放置策略未正确解析: %+v", configured.placementPolicy)
	}
}

func TestParseReconcileOptionsSupportsRuntimeHostingPrecedence(t *testing.T) {
	configured, err := parseReconcileOptions([]string{
		"-desired", "desired.json",
		"-runtime-hosting-default", "shared",
		"-publisher-runtime-hosting", "partner=dedicated",
		"-plugin-runtime-hosting", "cn.vastplan.heavy=shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.hostingPolicy.PublisherModes["partner"] != nodeagent.RuntimeHostingDedicated ||
		configured.hostingPolicy.PluginModes["cn.vastplan.heavy"] != nodeagent.RuntimeHostingShared {
		t.Fatalf("Runtime Host 策略未正确解析: %+v", configured.hostingPolicy)
	}
}

func TestParseReconcileOptionsValidatesCredentialRoot(t *testing.T) {
	configured, err := parseReconcileOptions([]string{"-desired", "desired.json", "-credential-root", "/etc/vastplan/credentials"})
	if err != nil || configured.credentialRoot != "/etc/vastplan/credentials" {
		t.Fatalf("规范 credential root 应被接受: %+v %v", configured, err)
	}
	if _, err := parseReconcileOptions([]string{"-desired", "desired.json", "-credential-root", "relative/credentials"}); err == nil {
		t.Fatal("相对 credential root 必须被拒绝")
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

func TestParseReconcileOptionsSupportsPublisherContextAccessPrecedence(t *testing.T) {
	configured, err := parseReconcileOptions([]string{
		"-desired", "desired.json",
		"-default-plugin-context-access", "scope.tenant,caller",
		"-publisher-plugin-context-access", "partner=scope.tenant,caller,trace;vastplan=*",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.contextPolicy.Ceiling("unknown").Has(callcontext.FieldTrace) ||
		!configured.contextPolicy.Ceiling("partner").Has(callcontext.FieldTrace) ||
		!configured.contextPolicy.Ceiling("vastplan").Has(callcontext.FieldGrantCredentials) {
		t.Fatalf("发布者上下文策略优先级错误: %+v", configured.contextPolicy)
	}
}

func TestBuildArtifactResolutionSeparatesLocalDevelopmentAndSignedBootstrap(t *testing.T) {
	local, err := parseReconcileOptions([]string{"-desired", "desired.json", "-repository", t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := buildArtifactResolution(local)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolution.sources) != 1 {
		t.Fatalf("本地开发模式应只有一个 file source: %+v", resolution.sources)
	}

	signedSeed, err := parseReconcileOptions([]string{
		"-desired", "desired.json", "-bootstrap-repository", t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildArtifactResolution(signedSeed); err == nil || !strings.Contains(err.Error(), "repository-trust") {
		t.Fatalf("签名种子源不得退化为无信任根模式: %v", err)
	}
}

func TestParseReconcileOptionsRequiresCompleteBootstrapUpgradeBoundary(t *testing.T) {
	tests := [][]string{
		{"-desired", "desired.json", "-bootstrap-upgrade"},
		{"-nats-url", "nats://127.0.0.1:4222", "-bootstrap-upgrade", "-bootstrap-repository", t.TempDir(), "-bootstrap-inventory", "/tmp/inventory.json"},
		{"-desired", "desired.json", "-bootstrap-upgrade", "-bootstrap-repository", t.TempDir(), "-bootstrap-inventory", "/tmp/inventory.json", "-repository-url", "https://repository.example"},
		{"-desired", "desired.json", "-publish-bootstrap-references", "-bootstrap-repository", t.TempDir(), "-bootstrap-inventory", "/tmp/inventory.json"},
	}
	for _, args := range tests {
		if _, err := parseReconcileOptions(args); err == nil {
			t.Fatalf("不完整的 Bootstrap 升级边界必须被拒绝: %v", args)
		}
	}
}

func TestParseReconcileOptionsAllowsBootstrapConsumerWithoutPublisher(t *testing.T) {
	options, err := parseReconcileOptions([]string{
		"-desired", "desired.json",
		"-bootstrap-repository", t.TempDir(),
		"-bootstrap-inventory", "/tmp/inventory.json",
	})
	if err != nil || options.publishBootstrapReferences || options.bootstrapUpgrade {
		t.Fatalf("普通节点应能只消费 Bootstrap Inventory: options=%+v err=%v", options, err)
	}
}

func TestParseReconcileOptionsRejectsConflictingOrInvalidPluginPolicies(t *testing.T) {
	tests := [][]string{
		{"-desired", "desired.json", "-third-party-plugin-policy", "deny", "-require-third-party-isolation=false"},
		{"-desired", "desired.json", "-third-party-plugin-policy", "invalid"},
		{"-desired", "desired.json", "-publisher-plugin-policies", "acme=deny,acme=allow-trusted"},
		{"-desired", "desired.json", "-plugin-placement-default", "in-process"},
		{"-desired", "desired.json", "-plugin-placements", "one=prefer-dynamic-go,one=process-only"},
		{"-desired", "desired.json", "-plugin-placements", "one=prefer-embedded"},
		{"-desired", "desired.json", "-default-plugin-context-access", "unknown"},
		{"-desired", "desired.json", "-publisher-plugin-context-access", "partner=unknown"},
		{"-desired", "desired.json", "-runtime-hosting-default", "elastic"},
		{"-desired", "desired.json", "-publisher-runtime-hosting", "partner=shared,partner=dedicated"},
		{"-desired", "desired.json", "-plugin-runtime-hosting", "missing-separator"},
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
