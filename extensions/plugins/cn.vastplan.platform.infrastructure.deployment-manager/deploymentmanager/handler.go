package deploymentmanager

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/configurationactivation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

func (s *Service) Handler(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	var result *contractv1.CallResult
	var raw []byte
	var handlerErr error
	err := s.withTenantState(ctx, host, call, func() error {
		result, raw, handlerErr = s.handleLoaded(ctx, host, call, payload, operation)
		return handlerErr
	})
	if err != nil {
		return domainError(errorCode(err), err)
	}
	return result, raw, handlerErr
}

func (s *Service) handleLoaded(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	var request handlerRequest
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return domainError("platform.deployment.invalid", errInvalid)
	}
	if err := ensureEOF(decoder); err != nil {
		return domainError("platform.deployment.invalid", errInvalid)
	}
	out, err := s.dispatchOperation(ctx, host, call, operation, request)
	if err != nil {
		return domainError(errorCode(err), err)
	}
	if revision, ok := out.(platformadminapi.ServiceRevision); ok {
		out = publicServiceRevision(revision)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errInvalid
	}
	return nil
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, errNotFound):
		return "platform.deployment.not_found"
	case errors.Is(err, errVersionConflict):
		return "platform.deployment.version_conflict"
	case errors.Is(err, errJobConflict):
		return "platform.deployment.job_conflict"
	case errors.Is(err, errSeparation):
		return "platform.deployment.separation_required"
	case errors.Is(err, errBootstrapFailed):
		return "platform.deployment.bootstrap_failed"
	case errors.Is(err, errPlanStale):
		return "platform.deployment.plan_stale"
	case errors.Is(err, errPlanNotReady):
		return "platform.deployment.plan_not_ready"
	case errors.Is(err, errServiceState):
		return "platform.deployment.service_state_conflict"
	case errors.Is(err, errServicePublish):
		return "platform.deployment.service_publish_failed"
	case errors.Is(err, errStoreConflict):
		return "platform.deployment.store_conflict"
	case errors.Is(err, errConfigurationActivation):
		return "platform.deployment.configuration_activation_failed"
	case errors.Is(err, errProfileActivation):
		return "platform.deployment.profile_configuration_activation_failed"
	case errors.Is(err, errTestBindingConflict):
		return "platform.test_release.binding_version_conflict"
	case errors.Is(err, errTestReleaseConflict):
		return "platform.test_release.in_progress"
	case errors.Is(err, errTestArtifact):
		return "platform.test_release.artifact_rejected"
	case errors.Is(err, plugininstallation.ErrUntrustedSource):
		return "platform.plugin_installation.source_untrusted"
	case errors.Is(err, plugininstallation.ErrTargetScopeMismatch):
		return "platform.plugin_installation.target_scope_mismatch"
	case errors.Is(err, plugininstallation.ErrDevelopmentForbidden):
		return "platform.plugin_installation.development_forbidden"
	case errors.Is(err, errInstallationConflict):
		return "platform.plugin_installation.change_conflict"
	case errors.Is(err, errInstallationUnsupported):
		return "platform.plugin_installation.unsupported"
	case errors.Is(err, errInstallationCandidateConflict):
		return "platform.plugin_installation.candidate_conflict"
	case errors.Is(err, errInstallationNoop):
		return "platform.plugin_installation.noop"
	case isSharedStateError(err):
		return "platform.deployment.unavailable"
	default:
		return "platform.deployment.invalid"
	}
}

func domainError(code string, err error) (*contractv1.CallResult, []byte, error) {
	retryable := errors.Is(err, errStoreConflict)
	var stateError *sharedstatesdk.ServiceError
	if errors.As(err, &stateError) {
		retryable = stateError.Retryable
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error(), Retryable: retryable}}, nil, nil
}

func isSharedStateError(err error) bool {
	var stateError *sharedstatesdk.ServiceError
	return errors.As(err, &stateError)
}

func randomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "bootstrap-" + hex.EncodeToString(raw[:]), nil
}

func Descriptor() []byte {
	return []byte(`{"title":"部署与测试发布管理","subcommands":[
		{"name":"listNodes","description":"列出当前租户的节点计划","paramsSchema":{"type":"object","properties":{}}},
		{"name":"putNode","description":"以 CAS 保存不含明文凭证的节点计划","paramsSchema":{"type":"object","properties":{"id":{"type":"string"},"plan":{"type":"object"},"ifVersion":{"type":"integer","minimum":0}},"required":["id","plan"]}},
		{"name":"listBootstrapJobs","description":"列出首次引导审批作业","paramsSchema":{"type":"object","properties":{}}},
		{"name":"createBootstrap","description":"申请指定节点的首次引导","paramsSchema":{"type":"object","properties":{"nodeId":{"type":"string"}},"required":["nodeId"]}},
		{"name":"approveBootstrap","description":"由不同审批人批准并触发可信内核引导","paramsSchema":{"type":"object","properties":{"jobId":{"type":"string"}},"required":["jobId"]}}
		,{"name":"listDeploymentTargets","description":"列出平台预授权的部署目标","paramsSchema":{"type":"object","properties":{}}}
		,{"name":"listServiceRevisions","description":"列出服务组合修订","paramsSchema":{"type":"object","properties":{}}}
		,{"name":"previewPluginInstallation","description":"按可信来源预览应用插件安装、升级或卸载影响","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"installationPreview":{"type":"object"}},"required":["installationPreview"]}}
		,{"name":"previewSelfServicePluginInstallation","description":"由 Portal 管理绑定为当前服务预览应用插件变更","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"installationPreview":{"type":"object"}},"required":["installationPreview"]}}
		,{"name":"previewDevelopmentPluginInstallation","description":"由受信开发控制器为显式绑定目标预览插件变更","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"installationPreview":{"type":"object"}},"required":["installationPreview"]}}
		,{"name":"createPluginInstallationCandidate","description":"从控制器入口创建持久插件安装候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"installationPreview":{"type":"object"}},"required":["installationPreview"]}}
		,{"name":"createSelfServicePluginInstallationCandidate","description":"从服务自助入口创建持久插件安装候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"installationPreview":{"type":"object"}},"required":["installationPreview"]}}
		,{"name":"createDevelopmentPluginInstallationCandidate","description":"从开发入口创建持久插件安装候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"installationPreview":{"type":"object"}},"required":["installationPreview"]}}
		,{"name":"listPluginInstallationTargets","description":"列出控制器获授权的最小逻辑服务安装目标","paramsSchema":{"type":"object","properties":{}}}
		,{"name":"listPluginInstallationCandidates","description":"列出插件安装候选及派生状态","paramsSchema":{"type":"object","properties":{}}}
		,{"name":"getPluginInstallationCandidate","description":"读取一个插件安装候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"submitPluginInstallationCandidate","description":"重新规划并提交插件安装候选审批","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"approvePluginInstallationCandidate","description":"由不同主体批准插件安装候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"activatePluginInstallationCandidate","description":"通过既有可信发布链激活插件安装候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"cancelPluginInstallationCandidate","description":"取消尚未提交的插件安装候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"rollbackPluginInstallationCandidate","description":"以新的服务修订回滚已激活安装候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"createIntentDraft","description":"创建 Application Intent 规划草稿","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"intent":{"type":"object"}},"required":["intent"]}}
		,{"name":"updateIntentDraft","description":"更新 Application Intent 并重建计划快照","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"revisionId":{"type":"integer","minimum":1},"intent":{"type":"object"}},"required":["revisionId","intent"]}}
		,{"name":"refreshIntentDraft","description":"显式接受最新 Planner 结果并清除 stale","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"revisionId":{"type":"integer","minimum":1}},"required":["revisionId"]}}
		,{"name":"bindIntentConfiguration","description":"由可信配置协调器绑定 CredentialRef 快照","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"revisionId":{"type":"integer","minimum":1},"configurationSnapshot":{"type":"object"}},"required":["revisionId","configurationSnapshot"]}}
		,{"name":"submitServiceDraft","description":"提交服务组合审批","paramsSchema":{"type":"object","properties":{"revisionId":{"type":"integer","minimum":1}},"required":["revisionId"]}}
		,{"name":"approveServiceRevision","description":"批准服务组合修订","paramsSchema":{"type":"object","properties":{"revisionId":{"type":"integer","minimum":1}},"required":["revisionId"]}}
		,{"name":"publishServiceRevision","description":"通过可信内核发布服务组合","paramsSchema":{"type":"object","properties":{"revisionId":{"type":"integer","minimum":1}},"required":["revisionId"]}}
		,{"name":"rollbackServiceRevision","description":"以新修订回滚到历史服务组合","paramsSchema":{"type":"object","properties":{"revisionId":{"type":"integer","minimum":1}},"required":["revisionId"]}}
		,{"name":"createConfigurationActivation","description":"从活动可信目录创建应用插件配置审批修订","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"activation":{"type":"object"}},"required":["activation"]}}
		,{"name":"getConfigurationActivation","description":"按配置候选读取外部发布与就绪状态","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"publishConfigurationActivation","description":"发布已审批配置修订并在 readiness 失败时单调回滚","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"createProfileConfigurationActivation","description":"从活动可信目录创建 Platform Profile 配置审批候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"profileActivation":{"type":"object"}},"required":["profileActivation"]}}
		,{"name":"getProfileConfigurationActivation","description":"读取 Platform Profile 配置激活状态","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"approveProfileConfigurationActivation","description":"由不同主体批准 Platform Profile 配置激活","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"publishProfileConfigurationActivation","description":"执行可恢复的 Catalog、Deployment 与 readiness 激活 Saga","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"abortProfileConfigurationActivation","description":"放弃尚未激活的 Platform Profile 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"candidateId":{"type":"string"}},"required":["candidateId"]}}
		,{"name":"listServiceRevisionAudit","description":"列出服务组合审计记录","paramsSchema":{"type":"object","properties":{"revisionId":{"type":"integer","minimum":1}},"required":["revisionId"]}}
		,{"name":"listTestTargetBindings","description":"列出 Backend 测试目标预授权绑定","paramsSchema":{"type":"object","properties":{}}}
		,{"name":"putTestTargetBinding","description":"以 CAS 保存 Backend 应用插件测试目标绑定","paramsSchema":{"type":"object","properties":{"id":{"type":"string"},"binding":{"type":"object"}},"required":["id","binding"]}}
		,{"name":"listTestReleases","description":"列出测试发布与自动回滚状态","paramsSchema":{"type":"object","properties":{}}}
		,{"name":"createTestRelease","description":"验证精确 testing 制品并执行候选发布","paramsSchema":{"type":"object","properties":{"release":{"type":"object"}},"required":["release"]}}
		,{"name":"rollbackTestRelease","description":"恢复回滚被中断的测试发布","paramsSchema":{"type":"object","properties":{"releaseId":{"type":"integer","minimum":1}},"required":["releaseId"]}}
	]}`)
}

func Contribution(service *Service) sdk.Contribution {
	handler := func(operation string) sdk.Handler {
		return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.Handler(ctx, host, call, payload, operation)
		}
	}
	operations := []string{"listNodes", "putNode", "listBootstrapJobs", "createBootstrap", "approveBootstrap", "listDeploymentTargets", "listServiceRevisions", plugininstallation.PreviewOperation, plugininstallation.SelfServicePreviewOperation, plugininstallation.DevelopmentPreviewOperation, plugininstallation.CreateOperation, plugininstallation.SelfServiceCreateOperation, plugininstallation.DevelopmentCreateOperation, plugininstallation.ListTargetsOperation, plugininstallation.ListOperation, plugininstallation.GetOperation, plugininstallation.SubmitOperation, plugininstallation.ApproveOperation, plugininstallation.ActivateOperation, plugininstallation.CancelOperation, plugininstallation.RollbackOperation, "createIntentDraft", "updateIntentDraft", "refreshIntentDraft", "bindIntentConfiguration", "submitServiceDraft", "approveServiceRevision", "publishServiceRevision", "rollbackServiceRevision", configurationactivation.CreateOperation, configurationactivation.GetOperation, configurationactivation.PublishOperation, platformprofileactivation.CreateActivationOperation, platformprofileactivation.GetActivationOperation, platformprofileactivation.ApproveActivationOperation, platformprofileactivation.PublishActivationOperation, platformprofileactivation.AbortActivationOperation, "listServiceRevisionAudit", "listTestTargetBindings", "putTestTargetBinding", "listTestReleases", "createTestRelease", "rollbackTestRelease"}
	handlers := make(map[string]sdk.Handler, len(operations))
	for _, operation := range operations {
		handlers[operation] = handler(operation)
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}
