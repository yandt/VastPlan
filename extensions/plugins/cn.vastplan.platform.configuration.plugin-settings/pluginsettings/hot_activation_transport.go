package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) resumeHotPreparation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, actor, id string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	record, ok := state.HotActivations[id]
	stages := cloneStages(state.CredentialStages[id])
	s.mu.Unlock()
	if !ok || record.Status != hotPreparing {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	if err := s.prepareCredentialStages(ctx, host, call, tenant, id, stages); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	observation, err := callHotController(ctx, host, call, record.Target, configurationv1.OperationPrepare, record.Prepare)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if err := validateHotPrepared(record, observation); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.checkpointHotPendingApproval(tenant, actor, id, observation)
}

func hotCredentialRefs(stages []credentialStage) map[string]pluginconfig.ManagedCredentialRef {
	if len(stages) == 0 {
		return nil
	}
	refs := make(map[string]pluginconfig.ManagedCredentialRef, len(stages))
	for _, stage := range stages {
		refs[stage.FieldID] = stage.Stage.Ref
	}
	return refs
}

func validateHotPrepared(record hotActivationRecord, observation configurationv1.Observation) error {
	if observation.ConfigurationID != record.Prepare.ConfigurationID || observation.Active != record.Prepare.ExpectedActive || observation.Candidate == nil ||
		observation.Candidate.CandidateID != record.Prepare.CandidateID || observation.Candidate.RequestDigest != record.RequestDigest ||
		observation.Candidate.Status != configurationv1.StatusPrepared || !observation.Candidate.Ready {
		return errors.New("configuration.v1 prepare 响应未绑定精确候选")
	}
	return nil
}

func getHotControllerStatus(ctx context.Context, host sdk.Host, call *contractv1.CallContext, target pluginconfiguration.ControllerTarget, request configurationv1.StatusRequest) (configurationv1.Observation, error) {
	return callHotController(ctx, host, call, target, configurationv1.OperationStatus, request)
}

func callHotController(ctx context.Context, host sdk.Host, call *contractv1.CallContext, target pluginconfiguration.ControllerTarget, operation string, request any) (configurationv1.Observation, error) {
	if host == nil || target.Protocol != configurationv1.Protocol || target.ExtensionPoint != configurationv1.ExtensionPoint || target.Capability == "" || target.LogicalService == "" {
		return configurationv1.Observation{}, errors.New("configuration.v1 可信目标无效")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return configurationv1.Observation{}, err
	}
	logicalService, routingDomain := target.LogicalService, target.RoutingDomain
	callTarget := &contractv1.CallTarget{ExtensionPoint: target.ExtensionPoint, Capability: target.Capability, Operation: &operation, LogicalService: &logicalService}
	if routingDomain != "" {
		callTarget.RoutingDomain = &routingDomain
	}
	result, raw, err := host.Call(ctx, callTarget, call, payload)
	if err != nil {
		return configurationv1.Observation{}, err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil {
			return configurationv1.Observation{}, errors.New(result.Error.Message)
		}
		return configurationv1.Observation{}, errors.New("configuration.v1 controller 拒绝调用")
	}
	var observation configurationv1.Observation
	if err := decodeStrict(raw, &observation); err != nil {
		return configurationv1.Observation{}, err
	}
	if err := configurationv1.ValidateObservation(observation); err != nil {
		return configurationv1.Observation{}, err
	}
	return observation, nil
}
