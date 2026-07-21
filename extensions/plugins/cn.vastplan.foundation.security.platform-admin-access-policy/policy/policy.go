// Package policy authorizes the narrow platform administration capability set.
package policy

import (
	"context"
	"encoding/json"
	"strings"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactstorage"
	v1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
)

const (
	PluginID      = "cn.vastplan.foundation.security.platform-admin-access-policy"
	PluginVersion = "0.14.0"
	Capability    = "foundation.security.platform-admin-access-policy"
)

func Check(_ context.Context, callCtx *v1.CallContext, payload []byte) (*v1.CallResult, []byte, error) {
	var request extpoint.PermissionRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, nil, err
	}
	decision, reason := decide(callCtx, request)
	raw, err := json.Marshal(extpoint.PermissionResponse{Decision: decision, Reason: reason})
	if err != nil {
		return nil, nil, err
	}
	return &v1.CallResult{Status: v1.CallResult_STATUS_OK}, raw, nil
}

func decide(c *v1.CallContext, request extpoint.PermissionRequest) (extpoint.Decision, string) {
	if c == nil || c.Caller == nil {
		return extpoint.DecisionDeny, "缺少经验证调用身份"
	}
	if allowedKernelCallback(c, request) {
		return extpoint.DecisionAllow, "平台基础插件受限宿主回调"
	}
	if databaseRuntimeAllowed(c, request) {
		return extpoint.DecisionAllow, "数据库管理面与受控数据面调用"
	}
	if artifactStorageAllowed(c, request) {
		return extpoint.DecisionAllow, "制品仓库 leader 可执行受限存储迁移"
	}
	if artifactReferenceWriteAllowed(c, request) {
		return extpoint.DecisionAllow, "制品消费者可发布自己拥有的完整引用快照"
	}
	if managedCredentialLifecycleAllowed(c, request) {
		return extpoint.DecisionAllow, "业务插件只能管理自己拥有的托管凭证"
	}
	if materialLeaseAllowed(c, request) {
		return extpoint.DecisionAllow, "可信宿主可申请绑定身份的一次性加密 material lease"
	}
	role := operationRole(request.Capability, request.Operation)
	if role == "" {
		if governedCapability(request.Capability) {
			return extpoint.DecisionDeny, "未知平台管理操作"
		}
		return extpoint.DecisionAbstain, "非平台管理能力"
	}
	if c.Caller.Kind == v1.CallerKind_CALLER_KIND_SYSTEM {
		return extpoint.DecisionAllow, "系统平台管理调用"
	}
	if c.Caller.Kind == v1.CallerKind_CALLER_KIND_PLUGIN {
		if pluginMetadataReadAllowed(c.Caller.Id, request.Capability, request.Operation) {
			return extpoint.DecisionAllow, "首方平台插件元数据读取"
		}
		// platform.settings 仍交给 bootstrap-policy 的命名空间基线。
		if request.Capability == platformadminapi.SettingsCapability {
			return extpoint.DecisionAbstain, "系统设置插件读取交给自举基线"
		}
		return extpoint.DecisionDeny, "插件不能继承用户的平台管理权限"
	}
	if c.Caller.Kind != v1.CallerKind_CALLER_KIND_USER {
		return extpoint.DecisionDeny, "仅已认证用户可管理平台资源"
	}
	if hasRole(c, "platform.admin") || hasRole(c, role) {
		return extpoint.DecisionAllow, "平台角色授权"
	}
	return extpoint.DecisionDeny, "缺少平台管理角色"
}

func artifactReferenceWriteAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if request.Capability != platformadminapi.ArtifactsCapability || request.Operation != "putReferences" {
		return false
	}
	if c.GetCaller().GetKind() == v1.CallerKind_CALLER_KIND_SYSTEM && strings.HasPrefix(c.GetCaller().GetId(), "node-agent/") {
		return true
	}
	if c.GetCaller().GetKind() != v1.CallerKind_CALLER_KIND_PLUGIN {
		return false
	}
	switch c.GetCaller().GetId() {
	case "cn.vastplan.platform.infrastructure.deployment-manager", "cn.vastplan.platform.configuration.portal-composer":
		return true
	default:
		return false
	}
}

func governedCapability(capability string) bool {
	switch capability {
	case platformadminapi.SettingsCapability, platformadminapi.CredentialsCapability, "platform.credentials.material-lease", "kernel.credential.material-lease", platformadminapi.DatabaseCapability, databasev1.Capability, platformadminapi.ArtifactsCapability, platformadminapi.DeploymentCapability:
		return true
	default:
		return strings.HasPrefix(capability, artifactstorage.CapabilityPrefix)
	}
}

func artifactStorageAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.GetCaller().GetKind() != v1.CallerKind_CALLER_KIND_PLUGIN || c.GetCaller().GetId() != "cn.vastplan.platform.artifacts.repository" || !strings.HasPrefix(request.Capability, artifactstorage.CapabilityPrefix) {
		return false
	}
	switch request.Operation {
	case "probe", "provision", "describe", "migrate", "release":
		return true
	default:
		return false
	}
}

func databaseRuntimeAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.GetCaller().GetKind() == v1.CallerKind_CALLER_KIND_PLUGIN && c.GetCaller().GetId() == databasev1.ConnectionManagerPluginID {
		return request.Capability == databasev1.Capability &&
			(request.Operation == databasev1.OperationActivate || request.Operation == databasev1.OperationRetire || request.Operation == databasev1.OperationProbe || request.Operation == databasev1.OperationProviders)
	}
	if c.GetCaller().GetKind() == v1.CallerKind_CALLER_KIND_PLUGIN && c.GetCaller().GetId() == databasev1.RuntimePluginID {
		return (request.Capability == platformadminapi.DatabaseCapability && request.Operation == "resolveRuntime") ||
			(request.Capability == databasev1.Capability && request.Operation == "transactionRelay")
	}
	if request.Capability != databasev1.Capability {
		return false
	}
	if request.Operation == databasev1.OperationProviders && c.GetCaller().GetId() != "" {
		return true
	}
	if request.Operation != databasev1.OperationQuery && request.Operation != databasev1.OperationExecute &&
		request.Operation != databasev1.OperationBegin && request.Operation != databasev1.OperationCommit && request.Operation != databasev1.OperationRollback {
		return false
	}
	switch c.GetCaller().GetKind() {
	case v1.CallerKind_CALLER_KIND_PLUGIN, v1.CallerKind_CALLER_KIND_AGENT, v1.CallerKind_CALLER_KIND_RUNNER, v1.CallerKind_CALLER_KIND_SYSTEM:
		return true
	default:
		return false
	}
}

func materialLeaseAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	return c.Caller.Kind == v1.CallerKind_CALLER_KIND_SYSTEM && c.Caller.Id != "" && request.Capability == "platform.credentials.material-lease" && request.Operation == "issue"
}

func managedCredentialLifecycleAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.Caller.Kind != v1.CallerKind_CALLER_KIND_PLUGIN || c.Caller.Id == "" || request.Capability != platformadminapi.CredentialsCapability {
		return false
	}
	switch request.Operation {
	case "stageManaged", "activateManaged", "abortManaged", "retireManaged":
		return true
	default:
		return false
	}
}

func operationRole(capability, operation string) string {
	roles := map[string]map[string]string{
		platformadminapi.SettingsCapability:    {"get": "platform.settings.read", "list": "platform.settings.read", "changesSince": "platform.settings.read", "put": "platform.admin", "delete": "platform.admin"},
		platformadminapi.CredentialsCapability: {"describe": "platform.credentials.read", "list": "platform.credentials.read", "put": "platform.credentials.write", "rotate": "platform.credentials.rotate", "revoke": "platform.credentials.revoke"},
		platformadminapi.DatabaseCapability:    {"describe": "platform.database.read", "list": "platform.database.read", "define": "platform.database.write", "remove": "platform.database.write", "probe": "platform.database.probe"},
		platformadminapi.ArtifactsCapability:   {"status": "platform.artifacts.read", "listCatalog": "platform.artifacts.read", "listPublishJournal": "platform.artifacts.read", "resolve": "platform.artifacts.read", "listReferences": "platform.artifacts.read", "setLifecycle": "platform.artifacts.lifecycle", "migrationStatus": "platform.artifacts.read", "prepareMigration": "platform.artifacts.migrate", "syncMigration": "platform.artifacts.migrate", "cutoverMigration": "platform.artifacts.migrate", "rollbackMigration": "platform.artifacts.migrate", "finalizeMigration": "platform.artifacts.migrate", "releaseMigration": "platform.artifacts.migrate"},
		platformadminapi.DeploymentCapability:  {"listNodes": "platform.deployment.read", "putNode": "platform.deployment.write", "listBootstrapJobs": "platform.deployment.read", "createBootstrap": "platform.deployment.bootstrap", "approveBootstrap": "platform.deployment.approve", "listDeploymentTargets": "platform.deployment.read", "listServiceRevisions": "platform.deployment.read", "listServiceRevisionAudit": "platform.deployment.read", "createServiceDraft": "platform.deployment.compose", "updateServiceDraft": "platform.deployment.compose", "submitServiceDraft": "platform.deployment.compose", "approveServiceRevision": "platform.deployment.approve", "publishServiceRevision": "platform.deployment.publish", "rollbackServiceRevision": "platform.deployment.publish", "listTestTargetBindings": "platform.deployment.read", "putTestTargetBinding": "platform.admin", "listTestReleases": "platform.deployment.read", "createTestRelease": "platform.deployment.publish", "rollbackTestRelease": "platform.deployment.publish"},
	}
	return roles[capability][operation]
}

func allowedKernelCallback(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.Caller.Kind != v1.CallerKind_CALLER_KIND_PLUGIN {
		return false
	}
	switch c.Caller.Id {
	case "cn.vastplan.platform.configuration.global-settings", "cn.vastplan.platform.security.credentials":
		return request.Capability == "kernel.config.get"
	case databasev1.RuntimePluginID:
		return request.Capability == "kernel.credential.material-lease"
	case "cn.vastplan.platform.infrastructure.deployment-manager":
		return request.Capability == "kernel.node.bootstrap" || request.Capability == "kernel.node.readiness" || request.Capability == "kernel.deployment.targets" || request.Capability == "kernel.deployment.preview" || request.Capability == "kernel.deployment.publish" || request.Capability == "kernel.deployment.readiness"
	default:
		return false
	}
}

func pluginMetadataReadAllowed(id, capability, operation string) bool {
	if len(id) < len("cn.vastplan.platform.") || id[:len("cn.vastplan.platform.")] != "cn.vastplan.platform." {
		return false
	}
	return operationRole(capability, operation) == "platform.credentials.read" || operationRole(capability, operation) == "platform.database.read" || operationRole(capability, operation) == "platform.artifacts.read"
}

func hasRole(c *v1.CallContext, role string) bool {
	for _, candidate := range c.GetPrincipal().GetSystemRoles() {
		if candidate == role {
			return true
		}
	}
	return false
}
