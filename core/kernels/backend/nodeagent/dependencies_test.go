package nodeagent

import (
	"context"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
)

func TestValidateRuntimeRequirements_LocalAndDegraded(t *testing.T) {
	plugins := []InstalledPlugin{
		{ID: "provider", Version: "1.2.0", Contract: PluginRuntimeContract{Contributions: []pluginv1.RuntimeContribution{{
			ExtensionPoint: "tool.package", ID: "platform.settings", InstancePolicy: "per-kernel", StateModel: "local-ephemeral", Visibility: "local", Routing: "direct",
		}}}},
		{ID: "consumer", Version: "1.0.0", Contract: PluginRuntimeContract{Requires: []pluginv1.RuntimeRequirement{
			{Capability: "platform.settings", Scope: "same-kernel", Kind: "strong", Ready: "readiness", FailurePolicy: "fail"},
			{Capability: "platform.cache", Scope: "remote", Kind: "soft", Ready: "readiness", FailurePolicy: "degrade"},
		}}},
	}
	degraded, err := validateRuntimeRequirements(context.Background(), plugins, nil, 10)
	if err != nil {
		t.Fatalf("本地强依赖应满足，软依赖可降级: %v", err)
	}
	if len(degraded) != 1 {
		t.Fatalf("预期一个降级依赖，实际 %v", degraded)
	}
}

func TestVersionsMatch(t *testing.T) {
	if !versionsMatch([]string{"1.2.3"}, "^1.0.0") {
		t.Fatal("semver range 应匹配")
	}
	if versionsMatch([]string{"2.0.0"}, "^1.0.0") {
		t.Fatal("不兼容版本不得匹配")
	}
}

func TestRequirementSatisfied_DataRejectsDegraded(t *testing.T) {
	requirement := pluginv1.RuntimeRequirement{Version: "^1.0.0", Kind: "data", Ready: "health"}
	if requirementSatisfied(addressing.Announcement{Version: "1.2.0", Readiness: "degraded"}, requirement) {
		t.Fatal("data 依赖不得以 degraded health 代替完整 readiness")
	}
	if !requirementSatisfied(addressing.Announcement{Version: "1.2.0", Readiness: "ready"}, requirement) {
		t.Fatal("ready 且版本兼容的 data 依赖应满足")
	}
}

func TestValidateInstalledPoliciesRejectsVisibilityElevation(t *testing.T) {
	deploymentPolicy := servicemodel.Normalize(servicemodel.Policy{
		InstancePolicy: "active-active", StateModel: "external-shared", Visibility: "global", Routing: "queue",
	})
	plugins := []InstalledPlugin{{ID: "settings", Contract: PluginRuntimeContract{Contributions: []pluginv1.RuntimeContribution{{
		ExtensionPoint: "tool.package", ID: "platform.settings", InstancePolicy: "active-active",
		StateModel: "external-shared", Visibility: "cluster", Routing: "queue",
	}}}}}
	if err := validateInstalledPolicies(deploymentPolicy, plugins); err == nil {
		t.Fatal("部署不得把 manifest 的 cluster capability 提升为 global")
	}
}
