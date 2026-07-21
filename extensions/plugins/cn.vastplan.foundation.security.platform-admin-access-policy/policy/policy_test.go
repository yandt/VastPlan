package policy

import (
	"testing"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
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
	if got, _ := decide(user("platform.deployment.compose"), extpoint.PermissionRequest{Capability: platformadminapi.DeploymentCapability, Operation: "publishServiceRevision"}); got != extpoint.DecisionDeny {
		t.Fatalf("服务组合编辑角色不能隐含发布: %s", got)
	}
	if got, _ := decide(user("platform.deployment.publish"), extpoint.PermissionRequest{Capability: platformadminapi.DeploymentCapability, Operation: "publishServiceRevision"}); got != extpoint.DecisionAllow {
		t.Fatalf("服务组合发布角色应允许: %s", got)
	}
	if got, _ := decide(user("platform.artifacts.migrate"), extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "cutoverMigration"}); got != extpoint.DecisionAllow {
		t.Fatalf("制品迁移角色应允许切换: %s", got)
	}
	if got, _ := decide(user("platform.artifacts.read"), extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "cutoverMigration"}); got != extpoint.DecisionDeny {
		t.Fatalf("制品读取角色不得隐含迁移: %s", got)
	}
}

func TestPlatformAdminDoesNotBecomeGenericPermissionPolicy(t *testing.T) {
	if got, _ := decide(user("platform.admin"), extpoint.PermissionRequest{Capability: "product.agent.run", Operation: "run"}); got != extpoint.DecisionAbstain {
		t.Fatalf("非平台能力必须弃权: %s", got)
	}
	plugin := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.platform.security.credentials"}}
	if got, _ := decide(plugin, extpoint.PermissionRequest{Capability: "kernel.config.get", Operation: "get"}); got != extpoint.DecisionAllow {
		t.Fatalf("受限回调应允许: %s", got)
	}
	if got, _ := decide(plugin, extpoint.PermissionRequest{Capability: platformadminapi.CredentialsCapability, Operation: "put"}); got != extpoint.DecisionDeny {
		t.Fatalf("插件不能继承写权限: %s", got)
	}
	businessPlugin := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "third.party.database"}}
	if got, _ := decide(businessPlugin, extpoint.PermissionRequest{Capability: platformadminapi.CredentialsCapability, Operation: "stageManaged"}); got != extpoint.DecisionAllow {
		t.Fatalf("业务插件应能创建自己拥有的托管凭证: %s", got)
	}
	if got, _ := decide(businessPlugin, extpoint.PermissionRequest{Capability: platformadminapi.CredentialsCapability, Operation: "revoke"}); got != extpoint.DecisionDeny {
		t.Fatalf("托管生命周期授权不能扩张为管理员撤销权限: %s", got)
	}
	lease := extpoint.PermissionRequest{Capability: "platform.credentials.material-lease", Operation: "issue"}
	if got, _ := decide(businessPlugin, lease); got != extpoint.DecisionDeny {
		t.Fatalf("普通插件不得申请 material lease: %s", got)
	}
	kernel := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "node-a"}}
	if got, _ := decide(kernel, lease); got != extpoint.DecisionAllow {
		t.Fatalf("可信宿主应可申请 material lease: %s", got)
	}
	deploymentPlugin := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.platform.infrastructure.deployment-manager"}}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: "kernel.node.bootstrap", Operation: "bootstrap"}); got != extpoint.DecisionAllow {
		t.Fatalf("deployment-manager 的受限内核回调应允许: %s", got)
	}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: "kernel.node.readiness", Operation: "observe"}); got != extpoint.DecisionAllow {
		t.Fatalf("deployment-manager 的节点就绪观察回调应允许: %s", got)
	}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: "kernel.deployment.publish", Operation: "execute"}); got != extpoint.DecisionAllow {
		t.Fatalf("deployment-manager 的可信发布回调应允许: %s", got)
	}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: "kernel.deployment.readiness", Operation: "execute"}); got != extpoint.DecisionAllow {
		t.Fatalf("deployment-manager 的部署就绪观察回调应允许: %s", got)
	}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "listCatalog"}); got != extpoint.DecisionAllow {
		t.Fatalf("deployment-manager 应只能读取制品目录元数据: %s", got)
	}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "resolve"}); got != extpoint.DecisionAllow {
		t.Fatalf("deployment-manager 应可生成精确制品锁: %s", got)
	}
	repositoryPlugin := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.platform.artifacts.repository"}}
	if got, _ := decide(repositoryPlugin, extpoint.PermissionRequest{Capability: "platform.artifacts.storage.file", Operation: "migrate"}); got != extpoint.DecisionAllow {
		t.Fatalf("repository must be allowed to invoke storage migration, got %s", got)
	}
	if got, _ := decide(businessPlugin, extpoint.PermissionRequest{Capability: "platform.artifacts.storage.file", Operation: "migrate"}); got != extpoint.DecisionDeny {
		t.Fatalf("business plugins must not invoke storage migration, got %s", got)
	}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "publish"}); got != extpoint.DecisionDeny {
		t.Fatalf("deployment-manager 不得取得仓库发布权限: %s", got)
	}
	databaseRuntime := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.foundation.data.relational.runtime"}}
	runtimeLease := extpoint.PermissionRequest{Capability: "kernel.credential.material-lease", Operation: "issue"}
	if got, _ := decide(databaseRuntime, runtimeLease); got != extpoint.DecisionAllow {
		t.Fatalf("Database Runtime 的本地加密 lease 中继应允许: %s", got)
	}
	if got, _ := decide(businessPlugin, runtimeLease); got != extpoint.DecisionDeny {
		t.Fatalf("其他插件不得被平台策略授权 Runtime lease: %s", got)
	}
}

func TestDatabaseRuntimeManagementAndExecutionBoundary(t *testing.T) {
	manager := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: databasev1.ConnectionManagerPluginID}}
	if got, _ := decide(manager, extpoint.PermissionRequest{Capability: databasev1.Capability, Operation: databasev1.OperationActivate}); got != extpoint.DecisionAllow {
		t.Fatalf("connection-manager 应可发布 Runtime 定义: %s", got)
	}
	if got, _ := decide(manager, extpoint.PermissionRequest{Capability: databasev1.Capability, Operation: databasev1.OperationQuery}); got != extpoint.DecisionDeny {
		t.Fatalf("connection-manager 不应继承查询能力: %s", got)
	}
	runtime := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: databasev1.RuntimePluginID}}
	if got, _ := decide(runtime, extpoint.PermissionRequest{Capability: platformadminapi.DatabaseCapability, Operation: "resolveRuntime"}); got != extpoint.DecisionAllow {
		t.Fatalf("Runtime 应可惰性解析连接定义: %s", got)
	}
	if got, _ := decide(runtime, extpoint.PermissionRequest{Capability: databasev1.Capability, Operation: "transactionRelay"}); got != extpoint.DecisionAllow {
		t.Fatalf("Runtime 实例之间应可精确转发事务: %s", got)
	}
	thirdParty := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "org.example.orders"}}
	if got, _ := decide(thirdParty, extpoint.PermissionRequest{Capability: databasev1.Capability, Operation: databasev1.OperationQuery}); got != extpoint.DecisionAllow {
		t.Fatalf("数据面调用应继续交由 Runtime 校验连接授权: %s", got)
	}
	if got, _ := decide(thirdParty, extpoint.PermissionRequest{Capability: databasev1.Capability, Operation: databasev1.OperationBegin}); got != extpoint.DecisionAllow {
		t.Fatalf("事务开始应继续交由 Runtime 校验连接授权: %s", got)
	}
	if got, _ := decide(thirdParty, extpoint.PermissionRequest{Capability: databasev1.Capability, Operation: "transactionRelay"}); got != extpoint.DecisionDeny {
		t.Fatalf("第三方插件不得伪造 Runtime 事务转发: %s", got)
	}
	if got, _ := decide(user("platform.admin"), extpoint.PermissionRequest{Capability: databasev1.Capability, Operation: databasev1.OperationQuery}); got != extpoint.DecisionDeny {
		t.Fatalf("用户不得直接执行底层 SQL: %s", got)
	}
}

func user(roles ...string) *contractv1.CallContext {
	return &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "user"}, Principal: &contractv1.Principal{SystemRoles: roles}}
}
