package deploymentmanager

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/configurationactivation"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
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
	var request struct {
		ID                    string                                             `json:"id"`
		NodeID                string                                             `json:"nodeId"`
		JobID                 string                                             `json:"jobId"`
		Plan                  nodebootstrap.Plan                                 `json:"plan"`
		IfVersion             *int64                                             `json:"ifVersion,omitempty"`
		RevisionID            uint64                                             `json:"revisionId"`
		ReleaseID             uint64                                             `json:"releaseId"`
		Intent                backendcompositionv1.ApplicationIntent             `json:"intent"`
		ConfigurationSnapshot backendcompositionv1.PlanningConfigurationSnapshot `json:"configurationSnapshot"`
		Binding               platformadminapi.PutTestTargetBindingRequest       `json:"binding"`
		Release               platformadminapi.CreateTestReleaseRequest          `json:"release"`
		Activation            configurationactivation.CreateRequest              `json:"activation"`
		ProfileActivation     platformprofileactivation.CreateActivationRequest  `json:"profileActivation"`
		CandidateID           string                                             `json:"candidateId"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return domainError("platform.deployment.invalid", errInvalid)
	}
	if err := ensureEOF(decoder); err != nil {
		return domainError("platform.deployment.invalid", errInvalid)
	}
	var out any
	var err error
	switch operation {
	case "listNodes":
		var items []platformadminapi.ManagedNode
		items, err = s.ListNodes(call)
		out = map[string]any{"items": items}
	case "putNode":
		out, err = s.PutNode(call, request.ID, platformadminapi.PutManagedNodeRequest{Plan: request.Plan, IfVersion: request.IfVersion})
	case "listBootstrapJobs":
		_ = s.refreshReadiness(ctx, host, call)
		var items []platformadminapi.BootstrapJob
		items, err = s.ListJobs(call)
		out = map[string]any{"items": items}
	case "createBootstrap":
		out, err = s.CreateJob(call, request.NodeID)
	case "approveBootstrap":
		var job platformadminapi.BootstrapJob
		var node platformadminapi.ManagedNode
		job, node, err = s.beginApproval(call, request.JobID)
		if err == nil {
			operationName := "bootstrap"
			raw, marshalErr := json.Marshal(nodebootstrap.ExecutionRequest{OperationID: job.ID, Plan: node.Plan})
			if marshalErr != nil {
				err = marshalErr
			} else {
				result, _, callErr := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: nodebootstrap.KernelService, Operation: &operationName}, call, raw)
				success := callErr == nil && result != nil && result.Status == contractv1.CallResult_STATUS_OK
				job, err = s.finishApproval(call, job.ID, success)
				if success && err == nil {
					_ = s.refreshReadiness(ctx, host, call)
					job, err = s.job(call, job.ID)
				}
				if !success && err == nil {
					err = errBootstrapFailed
				}
			}
		}
		out = job
	case "listDeploymentTargets":
		var items []platformadminapi.DeploymentTarget
		items, err = s.ListDeploymentTargets(ctx, host, call)
		out = map[string]any{"items": items}
	case "listServiceRevisions":
		_ = s.ReconcileServiceReferences(ctx, host, call)
		var items []platformadminapi.ServiceRevision
		items, err = s.ListServiceRevisions(call)
		out = map[string]any{"items": publicServiceRevisions(items)}
	case "createIntentDraft":
		out, err = s.CreateIntentDraft(ctx, host, call, request.Intent)
	case "updateIntentDraft":
		out, err = s.UpdateIntentDraft(ctx, host, call, request.RevisionID, request.Intent)
	case "refreshIntentDraft":
		out, err = s.RefreshIntentPlan(ctx, host, call, request.RevisionID)
	case "bindIntentConfiguration":
		out, err = s.BindIntentConfiguration(ctx, host, call, request.RevisionID, request.ConfigurationSnapshot)
	case "submitServiceDraft":
		out, err = s.SubmitServiceDraft(ctx, host, call, request.RevisionID)
	case "approveServiceRevision":
		out, err = s.ApproveServiceRevision(ctx, host, call, request.RevisionID)
	case "publishServiceRevision":
		out, err = s.PublishServiceRevision(ctx, host, call, request.RevisionID)
	case "rollbackServiceRevision":
		out, err = s.RollbackServiceRevision(ctx, host, call, request.RevisionID)
	case configurationactivation.CreateOperation:
		out, err = s.CreateConfigurationActivation(ctx, host, call, request.Activation)
	case configurationactivation.GetOperation:
		out, err = s.GetConfigurationActivation(ctx, host, call, configurationactivation.LookupRequest{CandidateID: request.CandidateID})
	case configurationactivation.PublishOperation:
		out, err = s.PublishConfigurationActivation(ctx, host, call, configurationactivation.LookupRequest{CandidateID: request.CandidateID})
	case platformprofileactivation.CreateActivationOperation:
		out, err = s.CreateProfileConfigurationActivation(ctx, host, call, request.ProfileActivation)
	case platformprofileactivation.GetActivationOperation:
		out, err = s.GetProfileConfigurationActivation(ctx, host, call, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	case platformprofileactivation.ApproveActivationOperation:
		out, err = s.ApproveProfileConfigurationActivation(call, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	case platformprofileactivation.PublishActivationOperation:
		out, err = s.PublishProfileConfigurationActivation(ctx, host, call, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	case platformprofileactivation.AbortActivationOperation:
		out, err = s.AbortProfileConfigurationActivation(ctx, host, call, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	case "listServiceRevisionAudit":
		_ = s.ReconcileServiceReferences(ctx, host, call)
		var items []platformadminapi.ServiceAuditEvent
		items, err = s.ListServiceRevisionAudit(call, request.RevisionID)
		out = map[string]any{"items": items}
	case "listTestTargetBindings":
		var items []platformadminapi.TestTargetBinding
		items, err = s.ListTestTargetBindings(call)
		out = map[string]any{"items": items}
	case "putTestTargetBinding":
		out, err = s.PutTestTargetBinding(call, request.ID, request.Binding)
	case "listTestReleases":
		var items []platformadminapi.TestRelease
		items, err = s.ListTestReleases(call)
		out = map[string]any{"items": items}
	case "createTestRelease":
		out, err = s.CreateTestRelease(ctx, host, call, request.Release)
	case "rollbackTestRelease":
		out, err = s.RollbackTestRelease(ctx, host, call, request.ReleaseID)
	default:
		err = errInvalid
	}
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
	operations := []string{"listNodes", "putNode", "listBootstrapJobs", "createBootstrap", "approveBootstrap", "listDeploymentTargets", "listServiceRevisions", "createIntentDraft", "updateIntentDraft", "refreshIntentDraft", "bindIntentConfiguration", "submitServiceDraft", "approveServiceRevision", "publishServiceRevision", "rollbackServiceRevision", configurationactivation.CreateOperation, configurationactivation.GetOperation, configurationactivation.PublishOperation, platformprofileactivation.CreateActivationOperation, platformprofileactivation.GetActivationOperation, platformprofileactivation.ApproveActivationOperation, platformprofileactivation.PublishActivationOperation, platformprofileactivation.AbortActivationOperation, "listServiceRevisionAudit", "listTestTargetBindings", "putTestTargetBinding", "listTestReleases", "createTestRelease", "rollbackTestRelease"}
	handlers := make(map[string]sdk.Handler, len(operations))
	for _, operation := range operations {
		handlers[operation] = handler(operation)
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}
