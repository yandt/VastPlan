package policy

import (
	"testing"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	configurationscopedv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationscoped/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
)

func TestUserRolesNeverFallThroughLegacyWorkloadPolicy(t *testing.T) {
	for _, test := range []extpoint.PermissionRequest{
		{Capability: platformadminapi.CredentialsCapability, Operation: "list"},
		{Capability: platformadminapi.DatabaseCapability, Operation: "probe"},
		{Capability: platformadminapi.DeploymentCapability, Operation: "approveBootstrap"},
		{Capability: platformadminapi.ArtifactsCapability, Operation: "cutoverMigration"},
		{Capability: "platform.api-exposure", Operation: "publish"},
	} {
		if got, _ := decide(user("platform.admin", "platform.credentials.read", "platform.database.probe", "platform.deployment.approve", "platform.artifacts.migrate", "platform.api-exposure.publish"), test); got != extpoint.DecisionDeny {
			t.Fatalf("用户权限必须由 Authorization Enforcer 判定，legacy policy 不得放行 %s/%s: %s", test.Capability, test.Operation, got)
		}
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
	for _, pluginID := range []string{"cn.vastplan.platform.configuration.global-settings", configurationauthority.CoordinatorPluginID, "cn.vastplan.platform.infrastructure.deployment-manager"} {
		plugin := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: pluginID}}
		for _, capability := range []string{"kernel.state.shared.get", "kernel.state.shared.create", "kernel.state.shared.update"} {
			if got, _ := decide(plugin, extpoint.PermissionRequest{Capability: capability}); got != extpoint.DecisionAllow {
				t.Fatalf("%s 的 Shared State 回调 %s 应允许: %s", pluginID, capability, got)
			}
		}
	}
	if got, _ := decide(businessPlugin, extpoint.PermissionRequest{Capability: "kernel.state.shared.get"}); got == extpoint.DecisionAllow {
		t.Fatalf("普通插件不得继承平台 Shared State 授权: %s", got)
	}
	for _, capability := range []string{
		platformprofileactivation.KernelPrepareService, platformprofileactivation.KernelStatusService,
		platformprofileactivation.KernelActivateService, platformprofileactivation.KernelPublishService,
		platformprofileactivation.KernelFinalizeService, platformprofileactivation.KernelAbortService,
		platformprofileactivation.KernelRollbackService,
	} {
		if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: capability, Operation: "execute"}); got != extpoint.DecisionAllow {
			t.Fatalf("deployment-manager 的 Profile Activation 回调 %s 应允许: %s", capability, got)
		}
		if got, _ := decide(businessPlugin, extpoint.PermissionRequest{Capability: capability, Operation: "execute"}); got == extpoint.DecisionAllow {
			t.Fatalf("普通插件不得调用 Profile Activation 回调 %s: %s", capability, got)
		}
	}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "listCatalog"}); got != extpoint.DecisionAllow {
		t.Fatalf("deployment-manager 应只能读取制品目录元数据: %s", got)
	}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "resolve"}); got != extpoint.DecisionAllow {
		t.Fatalf("deployment-manager 应可生成精确制品锁: %s", got)
	}
	if got, _ := decide(deploymentPlugin, extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "putReferences"}); got != extpoint.DecisionAllow {
		t.Fatalf("deployment-manager 应可发布自己的引用快照: %s", got)
	}
	configurationPlugin := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.platform.configuration.plugin-settings"}}
	if got, _ := decide(configurationPlugin, extpoint.PermissionRequest{Capability: "kernel.config.get", Operation: "get"}); got == extpoint.DecisionAllow {
		t.Fatalf("配置协调器迁移到 Shared State 后不得保留已删除的 kernel.config.get 授权: %s", got)
	}
	if got, _ := decide(configurationPlugin, extpoint.PermissionRequest{Capability: "kernel.configuration.catalogs", Operation: "list"}); got != extpoint.DecisionAllow {
		t.Fatalf("配置协调器必须能读取活动可信配置目录: %s", got)
	}
	if got, _ := decide(configurationPlugin, extpoint.PermissionRequest{Capability: configurationauthority.KernelIssueService, Operation: "issue"}); got != extpoint.DecisionAllow {
		t.Fatalf("配置协调器必须能申请精确的一次性配置授权: %s", got)
	}
	if got, _ := decide(configurationPlugin, extpoint.PermissionRequest{Capability: platformadminapi.CredentialsCapability, Operation: "stageDelegated"}); got != extpoint.DecisionAllow {
		t.Fatalf("配置协调器必须能暂存宿主授权的委托凭证: %s", got)
	}
	if got, _ := decide(configurationPlugin, extpoint.PermissionRequest{Capability: platformadminapi.CredentialsCapability, Operation: "prepareDelegated"}); got != extpoint.DecisionAllow {
		t.Fatalf("配置协调器必须能打开候选凭证窗口: %s", got)
	}
	if got, _ := decide(configurationPlugin, extpoint.PermissionRequest{Capability: platformadminapi.DeploymentCapability, Operation: configurationactivation.CreateOperation}); got != extpoint.DecisionAllow {
		t.Fatalf("配置协调器必须能创建候选绑定部署修订: %s", got)
	}
	for _, operation := range []string{
		platformprofileactivation.CreateActivationOperation, platformprofileactivation.GetActivationOperation,
		platformprofileactivation.ApproveActivationOperation, platformprofileactivation.PublishActivationOperation,
		platformprofileactivation.AbortActivationOperation,
	} {
		if got, _ := decide(configurationPlugin, extpoint.PermissionRequest{Capability: platformadminapi.DeploymentCapability, Operation: operation}); got != extpoint.DecisionAllow {
			t.Fatalf("配置协调器必须能调用 Profile Activation 操作 %s: %s", operation, got)
		}
		if got, _ := decide(businessPlugin, extpoint.PermissionRequest{Capability: platformadminapi.DeploymentCapability, Operation: operation}); got == extpoint.DecisionAllow {
			t.Fatalf("普通插件不得调用 Profile Activation 操作 %s: %s", operation, got)
		}
	}
	for _, operation := range []string{configurationv1.OperationPrepare, configurationv1.OperationCommit, configurationv1.OperationAbort, configurationv1.OperationStatus} {
		request := extpoint.PermissionRequest{ExtensionPoint: configurationv1.ExtensionPoint, Capability: "configuration.0123456789abcdef0123456789abcdef", Operation: operation}
		if got, _ := decide(configurationPlugin, request); got != extpoint.DecisionAllow {
			t.Fatalf("配置协调器必须能调用 configuration.v1 %s: %s", operation, got)
		}
		if got, _ := decide(businessPlugin, request); got != extpoint.DecisionDeny {
			t.Fatalf("普通插件不得调用 configuration.v1 %s: %s", operation, got)
		}
	}
	if got, _ := decide(configurationPlugin, extpoint.PermissionRequest{ExtensionPoint: extpoint.ToolPackage, Capability: "configuration.0123456789abcdef0123456789abcdef", Operation: configurationv1.OperationCommit}); got != extpoint.DecisionDeny {
		t.Fatalf("configuration.v1 必须绑定专用扩展点: %s", got)
	}
	scopedResolve := extpoint.PermissionRequest{ExtensionPoint: configurationscopedv1.ExtensionPoint, Capability: configurationscopedv1.Capability, Operation: configurationscopedv1.OperationResolve}
	if got, _ := decide(businessPlugin, scopedResolve); got != extpoint.DecisionAllow {
		t.Fatalf("插件应能调用自校验的 scoped resolver: %s", got)
	}
	if got, _ := decide(user("platform.admin"), scopedResolve); got != extpoint.DecisionDeny {
		t.Fatalf("用户不得直接调用 scoped resolver: %s", got)
	}
	scopedResolve.ExtensionPoint = extpoint.ToolPackage
	if got, _ := decide(businessPlugin, scopedResolve); got != extpoint.DecisionDeny {
		t.Fatalf("scoped resolver 不得退化为 tool.package: %s", got)
	}
	if got, _ := decide(businessPlugin, extpoint.PermissionRequest{Capability: platformadminapi.CredentialsCapability, Operation: "stageDelegated"}); got != extpoint.DecisionDeny {
		t.Fatalf("普通插件不得使用委托凭证入口: %s", got)
	}
	custodian := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: configurationauthority.CustodianPluginID}}
	if got, _ := decide(custodian, extpoint.PermissionRequest{Capability: configurationauthority.KernelConsumeService, Operation: "consume"}); got != extpoint.DecisionAllow {
		t.Fatalf("凭证托管器必须能原子消费配置授权: %s", got)
	}
	if got, _ := decide(configurationPlugin, extpoint.PermissionRequest{Capability: platformadminapi.DeploymentCapability, Operation: "listServiceRevisions"}); got != extpoint.DecisionDeny {
		t.Fatalf("配置协调器不得继承其他部署读取权限: %s", got)
	}
	if got, _ := decide(businessPlugin, extpoint.PermissionRequest{Capability: "kernel.config.credential-ref", Operation: "get"}); got != extpoint.DecisionAllow {
		t.Fatalf("业务插件应能读取宿主按自身身份投影的托管引用: %s", got)
	}
	if got, _ := decide(businessPlugin, extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "putReferences"}); got != extpoint.DecisionDeny {
		t.Fatalf("业务插件不得伪造平台引用快照: %s", got)
	}
	nodeAgent := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "node-agent/node-a"}}
	if got, _ := decide(nodeAgent, extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "putReferences"}); got != extpoint.DecisionAllow {
		t.Fatalf("可信 Node Agent 应可发布 Assignment 引用快照: %s", got)
	}
	bootstrapInventory := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "bootstrap-inventory/primary"}}
	if got, _ := decide(bootstrapInventory, extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "putReferences"}); got != extpoint.DecisionAllow {
		t.Fatalf("可信 Bootstrap Inventory 应可发布 Seed/LKG 引用快照: %s", got)
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

func TestAPIExposureManagementAndRuntimeBoundaries(t *testing.T) {
	plugin := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.example.data-plane"}}
	if got, _ := decide(plugin, extpoint.PermissionRequest{Capability: "platform.api-exposure", Operation: "registerEndpointLease"}); got != extpoint.DecisionAllow {
		t.Fatalf("插件应能登记自身 Lease: %s", got)
	}
	if got, _ := decide(plugin, extpoint.PermissionRequest{Capability: "platform.api-exposure", Operation: "publish"}); got != extpoint.DecisionDeny {
		t.Fatalf("插件不得发布 Exposure: %s", got)
	}
	if got, _ := decide(user("platform.artifacts.read"), extpoint.PermissionRequest{Capability: "platform.api-exposure", Operation: "issueDataPlaneTicket"}); got != extpoint.DecisionAllow {
		t.Fatalf("对象权限应交由 Exposure 服务复核: %s", got)
	}
	exposure := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.platform.integration.api-exposure"}}
	if got, _ := decide(exposure, extpoint.PermissionRequest{Capability: platformadminapi.ArtifactsCapability, Operation: "installDataPlaneTicket"}); got != extpoint.DecisionAllow {
		t.Fatalf("Exposure 控制面应能安装制品数据面 Ticket: %s", got)
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
