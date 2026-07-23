package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"

	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func listResourceItems(ctx context.Context, host sdk.Host, call *contractv1.CallContext, target pluginconfiguration.ControllerTarget, request configurationresourcev1.ListRequest) (configurationresourcev1.ListResponse, error) {
	raw, err := callResourceController(ctx, host, call, target, configurationresourcev1.OperationList, request)
	if err != nil {
		return configurationresourcev1.ListResponse{}, err
	}
	var response configurationresourcev1.ListResponse
	if err := decodeStrict(raw, &response); err != nil {
		return response, err
	}
	if err := configurationresourcev1.ValidateListResponse(response); err != nil || response.CollectionID != request.CollectionID {
		return response, errors.New("configuration.resource.v1 list 响应无效")
	}
	return response, nil
}

func getResourceItem(ctx context.Context, host sdk.Host, call *contractv1.CallContext, target pluginconfiguration.ControllerTarget, request configurationresourcev1.GetRequest) (configurationresourcev1.GetResponse, error) {
	raw, err := callResourceController(ctx, host, call, target, configurationresourcev1.OperationGet, request)
	if err != nil {
		return configurationresourcev1.GetResponse{}, err
	}
	var response configurationresourcev1.GetResponse
	if err := decodeStrict(raw, &response); err != nil {
		return response, err
	}
	if err := configurationresourcev1.ValidateGetResponse(response); err != nil || response.CollectionID != request.CollectionID || response.Item.ResourceID != request.ResourceID {
		return response, errors.New("configuration.resource.v1 get 响应无效")
	}
	return response, nil
}

func callResourceObservation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, target pluginconfiguration.ControllerTarget, operation string, request any) (configurationresourcev1.Observation, error) {
	raw, err := callResourceController(ctx, host, call, target, operation, request)
	if err != nil {
		return configurationresourcev1.Observation{}, err
	}
	var observation configurationresourcev1.Observation
	if err := decodeStrict(raw, &observation); err != nil {
		return observation, err
	}
	if err := configurationresourcev1.ValidateObservation(observation); err != nil {
		return observation, err
	}
	return observation, nil
}

func callResourceController(ctx context.Context, host sdk.Host, call *contractv1.CallContext, target pluginconfiguration.ControllerTarget, operation string, request any) ([]byte, error) {
	if host == nil || target.Protocol != configurationresourcev1.Protocol || target.ExtensionPoint != configurationresourcev1.ExtensionPoint || target.Capability == "" || target.LogicalService == "" {
		return nil, errors.New("configuration.resource.v1 可信目标无效")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	logicalService, routingDomain := target.LogicalService, target.RoutingDomain
	callTarget := &contractv1.CallTarget{ExtensionPoint: target.ExtensionPoint, Capability: target.Capability, Operation: &operation, LogicalService: &logicalService}
	if routingDomain != "" {
		callTarget.RoutingDomain = &routingDomain
	}
	result, raw, err := host.Call(ctx, callTarget, call, payload)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil {
			return nil, errors.New(result.Error.Message)
		}
		return nil, errors.New("configuration.resource.v1 controller 拒绝调用")
	}
	return raw, nil
}

func resourceCredentialRefs(stages []credentialStage) map[string]pluginconfig.ManagedCredentialRef {
	if len(stages) == 0 {
		return nil
	}
	refs := make(map[string]pluginconfig.ManagedCredentialRef, len(stages))
	for _, stage := range stages {
		refs[stage.FieldID] = stage.Stage.Ref
	}
	return refs
}

func (s *Service) resumeResourcePreparation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, actor, id string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	record, ok := state.ResourceActivations[id]
	stages := cloneStages(state.CredentialStages[id])
	s.mu.Unlock()
	if !ok || record.Status != resourcePreparing {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	if err := s.prepareCredentialStages(ctx, host, call, tenant, id, stages); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	observation, err := callResourceObservation(ctx, host, call, record.Target, configurationresourcev1.OperationPrepare, record.Prepare)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if err := validateResourcePrepared(record, observation); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.checkpointResourcePendingApproval(tenant, actor, id, observation)
}
