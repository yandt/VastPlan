package policy

import (
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
)

func TestPlatformAdminRolesAndUnknownOperations(t *testing.T) {
	read := extpoint.PermissionRequest{Capability: platformadminapi.CredentialsCapability, Operation: "list"}
	if got, _ := decide(user("platform.credentials.read"), read); got != extpoint.DecisionAllow {
		t.Fatalf("读角色应允许: %s", got)
	}
	if got, _ := decide(user("platform.credentials.write"), read); got != extpoint.DecisionDeny {
		t.Fatalf("写角色不能隐含读取: %s", got)
	}
	if got, _ := decide(user("platform.admin"), extpoint.PermissionRequest{Capability: platformadminapi.DatabaseCapability, Operation: "probe"}); got != extpoint.DecisionAllow {
		t.Fatalf("平台管理员应允许: %s", got)
	}
	if got, _ := decide(user("platform.admin"), extpoint.PermissionRequest{Capability: platformadminapi.DatabaseCapability, Operation: "future"}); got != extpoint.DecisionDeny {
		t.Fatalf("未知操作必须拒绝: %s", got)
	}
	if got, _ := decide(user("platform.deployment.bootstrap"), extpoint.PermissionRequest{Capability: platformadminapi.DeploymentCapability, Operation: "approveBootstrap"}); got != extpoint.DecisionDeny {
		t.Fatalf("引导申请角色不能隐含审批: %s", got)
	}
	if got, _ := decide(user("platform.deployment.approve"), extpoint.PermissionRequest{Capability: platformadminapi.DeploymentCapability, Operation: "approveBootstrap"}); got != extpoint.DecisionAllow {
		t.Fatalf("部署审批角色应允许: %s", got)
	}
}

func TestPlatformAdminDoesNotBecomeGenericPermissionPolicy(t *testing.T) {
	if got, _ := decide(user("platform.admin"), extpoint.PermissionRequest{Capability: "product.agent.run", Operation: "run"}); got != extpoint.DecisionAbstain {
		t.Fatalf("非平台能力必须弃权: %s", got)
	}
	plugin := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "com.vastplan.platform.security.credentials"}}
	if got, _ := decide(plugin, extpoint.PermissionRequest{Capability: "kernel.config.get", Operation: "get"}); got != extpoint.DecisionAllow {
		t.Fatalf("受限回调应允许: %s", got)
	}
	if got, _ := decide(plugin, extpoint.PermissionRequest{Capability: platformadminapi.CredentialsCapability, Operation: "put"}); got != extpoint.DecisionDeny {
		t.Fatalf("插件不能继承写权限: %s", got)
	}
	deploymentPlugin := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "com.vastplan.platform.infrastructure.deployment-manager"}}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: "kernel.node.bootstrap", Operation: "bootstrap"}); got != extpoint.DecisionAllow {
		t.Fatalf("deployment-manager 的受限内核回调应允许: %s", got)
	}
}

func user(roles ...string) *contractv1.CallContext {
	return &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "user"}, Principal: &contractv1.Principal{SystemRoles: roles}}
}
