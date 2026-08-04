package hostfactory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/internal/runtimeidentity"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/configurationauthority"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/operationfence"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfiguration"
)

func kernelConfigurationAuthorityIssue(issuer configurationauthority.Issuer) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != configurationauthority.CoordinatorPluginID || callCtx.GetTenantId() == "" {
			return nil, nil, errors.New("配置授权签发只接受 plugin-settings 认证会话")
		}
		var request configurationauthority.IssueRequest
		if err := decodeStrict(payload, &request); err != nil {
			return nil, nil, configurationauthority.ErrInvalid
		}
		issued, err := issuer.Issue(ctx, callCtx.GetTenantId(), request)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(issued)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelConfigurationAuthorityConsume(consumer configurationauthority.Consumer) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != configurationauthority.CustodianPluginID || callCtx.GetTenantId() == "" {
			return nil, nil, errors.New("配置授权消费只接受 credentials 认证会话")
		}
		var request struct {
			Token string `json:"token"`
		}
		if err := decodeStrict(payload, &request); err != nil {
			return nil, nil, configurationauthority.ErrInvalid
		}
		claims, err := consumer.Consume(ctx, callCtx.GetTenantId(), request.Token)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(claims)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelConfigurationCatalogs(reader pluginconfiguration.Reader) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != pluginconfiguration.PluginSettingsID || callCtx.GetTenantId() == "" {
			return nil, nil, errors.New("kernel.configuration.catalogs 只接受 plugin-settings 认证会话")
		}
		if err := decodeStrict(payload, &struct{}{}); err != nil {
			return nil, nil, errors.New("配置目录请求无效")
		}
		items, err := reader.List(ctx, callCtx.GetTenantId())
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(map[string]any{"items": items})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelRuntimeMaterialLease(broker kernelspi.RuntimeMaterialLeaseBroker) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		identity, ok := runtimeidentity.FromContext(ctx)
		if !ok || callCtx.GetTenantId() == "" || callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN ||
			callCtx.GetCaller().GetId() != identity.PluginID {
			return nil, nil, errors.New("kernel.credential.material-lease 缺少可信 Runtime 启动身份")
		}
		var request credentiallease.Request
		if err := decodeStrict(payload, &request); err != nil {
			return nil, nil, errors.New("runtime material lease 请求无效")
		}
		envelope, err := broker.IssueRuntimeLease(ctx, callCtx.GetTenantId(), identity, request)
		if err != nil {
			code, retryable, ok := credentiallease.FailureDetails(err)
			if !ok {
				code, retryable = credentiallease.ErrorServiceUnavailable, true
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{
				Code: code, Message: credentiallease.SafeFailureMessage(code), Retryable: retryable,
			}}, nil, nil
		}
		raw, err := json.Marshal(envelope)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

func authenticatedDeploymentManager(callCtx *contractv1.CallContext) bool {
	return callCtx.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_PLUGIN && callCtx.GetCaller().GetId() == deploymentpublication.DeploymentManagerPluginID && callCtx.GetTenantId() != ""
}

func deploymentManagerFence(ctx context.Context, callCtx *contractv1.CallContext, operationID string) (operationfence.Fence, error) {
	if !authenticatedDeploymentManager(callCtx) {
		return operationfence.Fence{}, errors.New("Deployment Manager execution fence 身份无效")
	}
	identity, ok := runtimeidentity.FromContext(ctx)
	if !ok || identity.Validate() != nil || identity.PluginID != deploymentpublication.DeploymentManagerPluginID {
		return operationfence.Fence{}, errors.New("Deployment Manager execution fence 缺少可信 Runtime 身份")
	}
	evidence, ok := operationfence.FromContext(ctx)
	if !ok || evidence.LogicalService != "platform.deployment" || evidence.UnitID != identity.RuntimeScope {
		return operationfence.Fence{}, errors.New("Deployment Manager 已失去当前 leader execution fence")
	}
	fence, err := evidence.ForOperation(operationID)
	if err != nil {
		return operationfence.Fence{}, errors.New("Deployment Manager operationId 无效")
	}
	return fence, nil
}

func kernelDeploymentTargets(controller deploymentpublication.Controller) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !authenticatedDeploymentManager(callCtx) {
			return nil, nil, errors.New("kernel.deployment.targets 只接受 deployment-manager 认证会话")
		}
		if err := decodeStrict(payload, &struct{}{}); err != nil {
			return nil, nil, errors.New("部署目标请求无效")
		}
		targets, err := controller.Targets(ctx, callCtx.GetTenantId())
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(map[string]any{"items": targets})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelDeploymentPreview(controller deploymentpublication.Controller) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !authenticatedDeploymentManager(callCtx) {
			return nil, nil, errors.New("kernel.deployment.preview 只接受 deployment-manager 认证会话")
		}
		var request deploymentpublication.PreviewRequest
		if err := decodeStrict(payload, &request); err != nil {
			return nil, nil, errors.New("部署预览请求无效")
		}
		result, err := controller.Preview(ctx, callCtx.GetTenantId(), request.Composition, request.DeploymentRevision)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(result)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelDeploymentPublish(controller deploymentpublication.Controller) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !authenticatedDeploymentManager(callCtx) {
			return nil, nil, errors.New("kernel.deployment.publish 只接受 deployment-manager 认证会话")
		}
		var request deploymentpublication.PublishRequest
		if err := decodeStrict(payload, &request); err != nil {
			return nil, nil, errors.New("部署发布请求无效")
		}
		if _, err := deploymentManagerFence(ctx, callCtx, fmt.Sprintf("deployment/%s/revision/%d", request.Composition.Metadata.Name, request.DeploymentRevision)); err != nil {
			return nil, nil, err
		}
		result, err := controller.Publish(ctx, callCtx.GetTenantId(), request.Composition, request.DeploymentRevision, request.ExpectedDigest)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(result)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelDeploymentReadiness(observer deploymentpublication.ReadinessObserver) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if !authenticatedDeploymentManager(callCtx) {
			return nil, nil, errors.New("kernel.deployment.readiness 只接受 deployment-manager 认证会话")
		}
		var request deploymentpublication.ReadinessRequest
		if err := decodeStrict(payload, &request); err != nil || request.DeploymentName == "" || request.DeploymentRevision == 0 {
			return nil, nil, errors.New("部署 readiness 请求无效")
		}
		observation, err := observer.Observe(ctx, callCtx.GetTenantId(), request.DeploymentName, request.DeploymentRevision)
		if err != nil {
			return nil, nil, err
		}
		raw, err := json.Marshal(observation)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
	}
}

func kernelPlatformProfileActivation(controller platformprofileactivation.Controller) map[string]protocolbus.HostService {
	candidate := func(action string, mutating bool, run func(context.Context, string, platformprofileactivation.CandidateRequest) (platformprofileactivation.Candidate, error)) protocolbus.HostService {
		return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if !authenticatedDeploymentManager(callCtx) {
				return nil, nil, errors.New("Platform Profile Activation 只接受 deployment-manager 认证会话")
			}
			var request platformprofileactivation.CandidateRequest
			if err := decodeStrict(payload, &request); err != nil {
				return nil, nil, errors.New("Platform Profile 候选请求无效")
			}
			if mutating {
				if _, err := deploymentManagerFence(ctx, callCtx, "platform-profile/"+request.CandidateID+"/"+action); err != nil {
					return nil, nil, err
				}
			}
			result, err := run(ctx, callCtx.GetTenantId(), request)
			if err != nil {
				return nil, nil, err
			}
			raw, err := json.Marshal(result)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
		}
	}
	return map[string]protocolbus.HostService{
		platformprofileactivation.KernelPrepareService: func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if !authenticatedDeploymentManager(callCtx) {
				return nil, nil, errors.New("Platform Profile Activation 只接受 deployment-manager 认证会话")
			}
			var request platformprofileactivation.PrepareRequest
			if err := decodeStrict(payload, &request); err != nil {
				return nil, nil, errors.New("Platform Profile 候选准备请求无效")
			}
			if _, err := deploymentManagerFence(ctx, callCtx, "platform-profile/"+request.CandidateID+"/prepare"); err != nil {
				return nil, nil, err
			}
			result, err := controller.Prepare(ctx, callCtx.GetTenantId(), request)
			if err != nil {
				return nil, nil, err
			}
			raw, err := json.Marshal(result)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
		},
		platformprofileactivation.KernelStatusService:   candidate("status", false, controller.Status),
		platformprofileactivation.KernelActivateService: candidate("activate", true, controller.Activate),
		platformprofileactivation.KernelFinalizeService: candidate("finalize", true, controller.Finalize),
		platformprofileactivation.KernelAbortService:    candidate("abort", true, controller.Abort),
		platformprofileactivation.KernelRollbackService: candidate("rollback", true, controller.Rollback),
		platformprofileactivation.KernelPublishService: func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if !authenticatedDeploymentManager(callCtx) {
				return nil, nil, errors.New("Platform Profile Activation 只接受 deployment-manager 认证会话")
			}
			var request platformprofileactivation.PublishRequest
			if err := decodeStrict(payload, &request); err != nil {
				return nil, nil, errors.New("Platform Profile 候选发布请求无效")
			}
			if _, err := deploymentManagerFence(ctx, callCtx, "platform-profile/"+request.Prepare.CandidateID+"/publish"); err != nil {
				return nil, nil, err
			}
			result, err := controller.Publish(ctx, callCtx.GetTenantId(), request)
			if err != nil {
				return nil, nil, err
			}
			raw, err := json.Marshal(result)
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
		},
	}
}
