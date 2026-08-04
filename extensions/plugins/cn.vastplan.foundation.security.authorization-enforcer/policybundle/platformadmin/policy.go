// Package platformadmin authorizes the narrow platform administration
// capability set inside the shared Authorization Enforcer process.
package platformadmin

import (
	"context"
	"encoding/json"
	"strings"

	v1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	configurationscopedv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationscoped/v1"
	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	sharedstatev1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstate/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactstorage"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/configurationactivation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/configurationauthority"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfiguration"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginmarketplace"
)

const (
	Capability = "foundation.security.platform-admin-access-policy"
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
	if c.Caller.Kind == v1.CallerKind_CALLER_KIND_SYSTEM && request.Capability == "foundation.security.authorization-engine.native" {
		return extpoint.DecisionAllow, "可信宿主可调用本地 Native Authorization Engine"
	}
	if allowedKernelCallback(c, request) {
		return extpoint.DecisionAllow, "平台基础插件受限宿主回调"
	}
	if databaseRuntimeAllowed(c, request) {
		return extpoint.DecisionAllow, "数据库管理面与受控数据面调用"
	}
	if apiExposureRuntimeAllowed(c, request) {
		return extpoint.DecisionAllow, "插件可登记自身已发布数据面的短租约或消费绑定 Ticket"
	}
	if apiExposureTicketInstallationAllowed(c, request) {
		return extpoint.DecisionAllow, "API Exposure 控制面可向精确数据面安装短时一次性 Ticket"
	}
	if apiExposureTicketAllowed(c, request) {
		return extpoint.DecisionAllow, "已认证主体的数据面权限由 Exposure 服务做对象级复核"
	}
	if artifactStorageAllowed(c, request) {
		return extpoint.DecisionAllow, "制品仓库 leader 可执行受限存储迁移"
	}
	if artifactReferenceWriteAllowed(c, request) {
		return extpoint.DecisionAllow, "制品消费者可发布自己拥有的完整引用快照"
	}
	if marketplaceCatalogReadAllowed(c, request) {
		return extpoint.DecisionAllow, "Marketplace 只能读取已验证 Artifact Catalog"
	}
	if managedCredentialLifecycleAllowed(c, request) {
		return extpoint.DecisionAllow, "业务插件只能管理自己拥有的托管凭证"
	}
	if delegatedManagedCredentialLifecycleAllowed(c, request) {
		return extpoint.DecisionAllow, "配置协调器只能操作宿主授权绑定的委托凭证"
	}
	if configurationActivationAllowed(c, request) {
		return extpoint.DecisionAllow, "配置协调器只能驱动候选绑定的应用配置发布"
	}
	if portalInstallationAllowed(c, request) {
		return extpoint.DecisionAllow, "部署管理器只能驱动显式 Portal 目标的插件安装事务"
	}
	if compositionPlanningAllowed(c, request) {
		return extpoint.DecisionAllow, "Deployment Manager 只能调用无副作用的应用组合规划端口"
	}
	if configurationControllerAllowed(c, request) {
		return extpoint.DecisionAllow, "配置协调器只能调用目标插件的标准 Hot Service 控制端口"
	}
	if scopedConfigurationResolveAllowed(c, request) {
		return extpoint.DecisionAllow, "插件只能在可信 tenant/subject 上下文中读取自身 Scoped Configuration"
	}
	if pluginConfigurationCatalogReadAllowed(c, request) {
		return extpoint.DecisionAllow, "插件配置协调器只能读取活动可信配置目录"
	}
	if materialLeaseAllowed(c, request) {
		return extpoint.DecisionAllow, "可信宿主可申请绑定身份的一次性加密 material lease"
	}
	if c.Caller.Kind == v1.CallerKind_CALLER_KIND_SYSTEM {
		if governedCapability(request.Capability) {
			return extpoint.DecisionAllow, "系统平台管理调用"
		}
		return extpoint.DecisionAbstain, "非平台管理能力"
	}
	if c.Caller.Kind == v1.CallerKind_CALLER_KIND_PLUGIN {
		if pluginMetadataReadAllowed(c.Caller.Id, request.Capability, request.Operation) {
			return extpoint.DecisionAllow, "首方平台插件元数据读取"
		}
		// platform.settings 仍交给 bootstrap-policy 的命名空间基线。
		if request.Capability == platformadminapi.SettingsCapability {
			return extpoint.DecisionAbstain, "系统设置插件读取交给自举基线"
		}
		if governedCapability(request.Capability) {
			return extpoint.DecisionDeny, "插件不能继承用户的平台管理权限"
		}
		return extpoint.DecisionAbstain, "非平台管理能力"
	}
	if c.Caller.Kind == v1.CallerKind_CALLER_KIND_USER && governedCapability(request.Capability) {
		return extpoint.DecisionDeny, "用户授权必须由签名 Permission Catalog 与 Authorization Enforcer 判定"
	}
	if c.Caller.Kind != v1.CallerKind_CALLER_KIND_USER && governedCapability(request.Capability) {
		return extpoint.DecisionDeny, "仅已认证用户可管理平台资源"
	}
	return extpoint.DecisionAbstain, "非平台管理能力"
}

func portalInstallationAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.GetCaller().GetKind() != v1.CallerKind_CALLER_KIND_PLUGIN || c.GetCaller().GetId() != "cn.vastplan.platform.infrastructure.deployment-manager" || request.ExtensionPoint != extpoint.ToolPackage || request.Capability != "platform.portal-composer" {
		return false
	}
	switch request.Operation {
	case "preparePluginInstallation", "commitPluginInstallation", "abortPluginInstallation", "rollbackPluginInstallation":
		return true
	default:
		return false
	}
}

func scopedConfigurationResolveAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.GetCaller().GetKind() != v1.CallerKind_CALLER_KIND_PLUGIN || request.ExtensionPoint != configurationscopedv1.ExtensionPoint || request.Capability != configurationscopedv1.Capability {
		return false
	}
	return request.Operation == configurationscopedv1.OperationResolve || request.Operation == configurationscopedv1.OperationWatchRevision
}

func artifactReferenceWriteAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if request.Capability != platformadminapi.ArtifactsCapability || request.Operation != "putReferences" {
		return false
	}
	if c.GetCaller().GetKind() == v1.CallerKind_CALLER_KIND_SYSTEM && (strings.HasPrefix(c.GetCaller().GetId(), "node-agent/") || strings.HasPrefix(c.GetCaller().GetId(), "bootstrap-inventory/")) {
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
	case platformadminapi.SettingsCapability, platformadminapi.CredentialsCapability, "platform.credentials.material-lease", "kernel.credential.material-lease", configurationauthority.KernelIssueService, configurationauthority.KernelConsumeService, platformadminapi.DatabaseCapability, databasev1.Capability, platformadminapi.ArtifactsCapability, pluginmarketplace.Capability, platformadminapi.DeploymentCapability, platformadminapi.PluginConfigurationCapability, backendcompositionv1.PlanningCapability, "platform.api-exposure":
		return true
	default:
		return strings.HasPrefix(capability, artifactstorage.CapabilityPrefix) || strings.HasPrefix(capability, "configuration.")
	}
}

func marketplaceCatalogReadAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	return c.GetCaller().GetKind() == v1.CallerKind_CALLER_KIND_PLUGIN && c.GetCaller().GetId() == "cn.vastplan.platform.artifacts.marketplace" && request.ExtensionPoint == extpoint.ToolPackage && request.Capability == platformadminapi.ArtifactsCapability && request.Operation == "listCatalog"
}

func compositionPlanningAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	return c.GetCaller().GetKind() == v1.CallerKind_CALLER_KIND_PLUGIN &&
		c.GetCaller().GetId() == "cn.vastplan.platform.infrastructure.deployment-manager" &&
		request.ExtensionPoint == extpoint.ToolPackage && request.Capability == backendcompositionv1.PlanningCapability &&
		request.Operation == backendcompositionv1.PlanningOperation
}

func configurationControllerAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.GetCaller().GetKind() != v1.CallerKind_CALLER_KIND_PLUGIN || c.GetCaller().GetId() != pluginconfiguration.PluginSettingsID || !strings.HasPrefix(request.Capability, "configuration.") {
		return false
	}
	if request.ExtensionPoint == configurationv1.ExtensionPoint {
		switch request.Operation {
		case configurationv1.OperationPrepare, configurationv1.OperationCommit, configurationv1.OperationAbort, configurationv1.OperationStatus:
			return true
		}
	}
	if request.ExtensionPoint == configurationresourcev1.ExtensionPoint {
		switch request.Operation {
		case configurationresourcev1.OperationList, configurationresourcev1.OperationGet, configurationresourcev1.OperationPrepare,
			configurationresourcev1.OperationCommit, configurationresourcev1.OperationAbort, configurationresourcev1.OperationStatus:
			return true
		}
	}
	return false
}

func pluginConfigurationCatalogReadAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	return c.GetCaller().GetKind() == v1.CallerKind_CALLER_KIND_PLUGIN &&
		c.GetCaller().GetId() == pluginconfiguration.PluginSettingsID &&
		request.Capability == pluginconfiguration.KernelCatalogsService && request.Operation == "list"
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

func apiExposureRuntimeAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.GetCaller().GetId() == "" || request.Capability != "platform.api-exposure" {
		return false
	}
	if request.Operation == "issuePrivateDataPlaneTicket" {
		kind := c.GetCaller().GetKind()
		return c.GetPrincipal().GetUserId() != "" && (kind == v1.CallerKind_CALLER_KIND_PLUGIN || kind == v1.CallerKind_CALLER_KIND_RUNNER || kind == v1.CallerKind_CALLER_KIND_SYSTEM)
	}
	if c.GetCaller().GetKind() != v1.CallerKind_CALLER_KIND_PLUGIN {
		return false
	}
	switch request.Operation {
	case "registerEndpointLease", "renewEndpointLease", "revokeEndpointLease", "consumeDataPlaneTicket":
		return true
	default:
		return false
	}
}

func apiExposureTicketAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	return c.GetCaller().GetKind() == v1.CallerKind_CALLER_KIND_USER && c.GetCaller().GetId() != "" && request.Capability == "platform.api-exposure" && request.Operation == "issueDataPlaneTicket"
}

func apiExposureTicketInstallationAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.GetCaller().GetKind() != v1.CallerKind_CALLER_KIND_PLUGIN || c.GetCaller().GetId() != "cn.vastplan.platform.integration.api-exposure" {
		return false
	}
	if request.Capability == platformadminapi.ArtifactsCapability {
		return request.Operation == "installDataPlaneTicket" || request.Operation == "installAssessmentReportTicket"
	}
	return request.Capability == stagingv1.UploadDataPlaneCapability && request.Operation == stagingv1.OperationInstallDataPlaneTicket
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

func delegatedManagedCredentialLifecycleAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.Caller.Kind != v1.CallerKind_CALLER_KIND_PLUGIN || c.Caller.Id != configurationauthority.CoordinatorPluginID || request.Capability != platformadminapi.CredentialsCapability {
		return false
	}
	switch request.Operation {
	case "stageDelegated", "prepareDelegated", "activateDelegated", "abortDelegated":
		return true
	default:
		return false
	}
}

func configurationActivationAllowed(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.GetCaller().GetKind() != v1.CallerKind_CALLER_KIND_PLUGIN || c.GetCaller().GetId() != pluginconfiguration.PluginSettingsID || request.Capability != platformadminapi.DeploymentCapability {
		return false
	}
	switch request.Operation {
	case configurationactivation.CreateOperation, configurationactivation.GetOperation, configurationactivation.PublishOperation,
		platformprofileactivation.CreateActivationOperation, platformprofileactivation.GetActivationOperation,
		platformprofileactivation.ApproveActivationOperation, platformprofileactivation.PublishActivationOperation,
		platformprofileactivation.AbortActivationOperation, "bindIntentConfiguration":
		return true
	default:
		return false
	}
}

func allowedKernelCallback(c *v1.CallContext, request extpoint.PermissionRequest) bool {
	if c.Caller.Kind != v1.CallerKind_CALLER_KIND_PLUGIN {
		return false
	}
	if request.Capability == pluginconfig.KernelCredentialRefService && request.Operation == "get" {
		return true
	}
	switch c.Caller.Id {
	case "cn.vastplan.platform.configuration.global-settings":
		return sharedStateKernelService(request.Capability)
	case configurationauthority.CoordinatorPluginID:
		return request.Capability == configurationauthority.KernelIssueService || sharedStateKernelService(request.Capability)
	case configurationauthority.CustodianPluginID:
		return request.Capability == configurationauthority.KernelConsumeService || credentialsSharedStateKernelService(request.Capability)
	case "cn.vastplan.platform.security.authorization-policy":
		return authorizationPolicySharedStateKernelService(request.Capability)
	case databasev1.RuntimePluginID:
		return request.Capability == "kernel.credential.material-lease"
	case databasev1.ConnectionManagerPluginID:
		return request.Capability == platformcontrolv1.KernelStatusService || request.Capability == platformcontrolv1.KernelTestService || request.Capability == platformcontrolv1.KernelConfigureService
	case "cn.vastplan.platform.artifacts.marketplace":
		return request.Capability == "kernel.credential.material-lease" && request.Operation == "issue"
	case "cn.vastplan.platform.infrastructure.deployment-manager":
		return request.Capability == "kernel.node.bootstrap" || request.Capability == "kernel.node.readiness" || request.Capability == "kernel.deployment.targets" || request.Capability == "kernel.deployment.preview" || request.Capability == "kernel.deployment.publish" || request.Capability == "kernel.deployment.readiness" || platformProfileKernelService(request.Capability) || sharedStateKernelService(request.Capability)
	default:
		return false
	}
}

func authorizationPolicySharedStateKernelService(capability string) bool {
	return capability == sharedstatev1.KernelService(sharedstatev1.OperationGet) ||
		capability == sharedstatev1.FencedKernelService(sharedstatev1.OperationCreate) ||
		capability == sharedstatev1.FencedKernelService(sharedstatev1.OperationUpdate)
}

func sharedStateKernelService(capability string) bool {
	return capability == sharedstatev1.KernelService(sharedstatev1.OperationGet) || capability == sharedstatev1.KernelService(sharedstatev1.OperationCreate) || capability == sharedstatev1.KernelService(sharedstatev1.OperationUpdate)
}

func credentialsSharedStateKernelService(capability string) bool {
	return capability == sharedstatev1.KernelService(sharedstatev1.OperationGet) || capability == sharedstatev1.KernelService(sharedstatev1.OperationList) ||
		capability == sharedstatev1.FencedKernelService(sharedstatev1.OperationCreate) || capability == sharedstatev1.FencedKernelService(sharedstatev1.OperationUpdate) ||
		capability == sharedstatev1.FencedKernelService(sharedstatev1.OperationDelete)
}

func platformProfileKernelService(capability string) bool {
	switch capability {
	case platformprofileactivation.KernelPrepareService, platformprofileactivation.KernelStatusService,
		platformprofileactivation.KernelActivateService, platformprofileactivation.KernelPublishService,
		platformprofileactivation.KernelFinalizeService, platformprofileactivation.KernelAbortService,
		platformprofileactivation.KernelRollbackService:
		return true
	default:
		return false
	}
}

func pluginMetadataReadAllowed(id, capability, operation string) bool {
	if len(id) < len("cn.vastplan.platform.") || id[:len("cn.vastplan.platform.")] != "cn.vastplan.platform." {
		return false
	}
	// Marketplace has an intentionally narrower repository projection and must
	// pass marketplaceCatalogReadAllowed instead of inheriting all platform reads.
	if id == "cn.vastplan.platform.artifacts.marketplace" {
		return false
	}
	switch capability {
	case platformadminapi.CredentialsCapability:
		return operation == "describe" || operation == "list"
	case platformadminapi.DatabaseCapability:
		return operation == "describe" || operation == "list"
	case platformadminapi.ArtifactsCapability:
		if operation == "resolve" {
			return id == "cn.vastplan.platform.infrastructure.deployment-manager"
		}
		if operation == "describePlanning" {
			return id == "cn.vastplan.platform.infrastructure.composition-planner"
		}
		return operation == "status" || operation == "capacity" || operation == "listCatalog" || operation == "listPublishJournal" || operation == "listReferences" || operation == "gcPlan" || operation == "gcStatus" || operation == "migrationStatus"
	default:
		return false
	}
}
