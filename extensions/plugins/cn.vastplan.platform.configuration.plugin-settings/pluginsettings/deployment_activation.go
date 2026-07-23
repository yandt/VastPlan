package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/configurationactivation"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const deploymentLogicalService = "platform.deployment"

func (s *Service) SubmitDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	draft, err := s.candidateSnapshot(tenant, id, expectedRevision, pluginconfiguration.CandidateDraft)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	definition, err := s.currentDefinition(ctx, host, call, draft)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if definition.ApplyPath != pluginconfiguration.ApplyApplicationDeployment {
		return pluginconfiguration.Candidate{}, fmt.Errorf("%w: 当前阶段只支持 Application Deployment 配置", ErrInvalid)
	}
	candidate, stages, err := s.beginSubmission(tenant, actor, id, expectedRevision)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	activation, err := createDeploymentActivation(ctx, host, call, definition, candidate, stages)
	if err != nil {
		// Publishing with no external revision is a durable unknown-outcome
		// checkpoint. Recovery retries the candidate-idempotent create call.
		return pluginconfiguration.Candidate{}, err
	}
	return s.checkpointExternal(tenant, candidate.ID, actor, activation)
}

func (s *Service) candidateSnapshot(tenant, id string, expectedRevision uint64, status pluginconfiguration.CandidateStatus) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate, ok := s.tenantLocked(tenant).Candidates[id]
	if !ok {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.Revision != expectedRevision || candidate.Status != status {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) ActivateCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	activation, err := getDeploymentActivation(ctx, host, call, id)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if activation.Status != configurationactivation.StatusApproved {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	candidate, stages, err := s.beginActivation(tenant, actor, id, expectedRevision, activation)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if err := s.prepareCredentialStages(ctx, host, call, tenant, candidate.ID, stages); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	activation, err = publishDeploymentActivation(ctx, host, call, candidate.ID)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.completeExternalActivation(ctx, host, call, tenant, actor, candidate.ID, activation)
}

func (s *Service) currentDefinition(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidate pluginconfiguration.Candidate) (pluginconfiguration.Definition, error) {
	catalogs, err := s.catalogs(ctx, host, call)
	if err != nil {
		return pluginconfiguration.Definition{}, err
	}
	view, err := findDefinition(catalogs, candidate.ConfigurationID, candidate.CatalogDigest)
	if err != nil {
		return pluginconfiguration.Definition{}, err
	}
	definition := view.Definition
	if definition.SchemaDigest != candidate.SchemaDigest || definition.Artifact.SHA256 != candidate.ArtifactSHA256 {
		return pluginconfiguration.Definition{}, ErrConflict
	}
	return definition, nil
}

func createDeploymentActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, definition pluginconfiguration.Definition, candidate pluginconfiguration.Candidate, stages []credentialStage) (configurationactivation.Activation, error) {
	credentials := make(map[string]pluginconfig.ManagedCredentialRef, len(stages))
	for _, binding := range stages {
		credentials[binding.FieldID] = binding.Stage.Ref
	}
	request := configurationactivation.CreateRequest{
		CandidateID: candidate.ID, ConfigurationID: candidate.ConfigurationID, CatalogDigest: candidate.CatalogDigest,
		SchemaDigest: candidate.SchemaDigest, ArtifactSHA256: candidate.ArtifactSHA256,
		Values: append(json.RawMessage(nil), candidate.Values...), Credentials: credentials,
	}
	var activation configurationactivation.Activation
	err := callDeploymentActivation(ctx, host, call, configurationactivation.CreateOperation, map[string]any{"activation": request}, &activation)
	return activation, err
}

func getDeploymentActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidateID string) (configurationactivation.Activation, error) {
	var activation configurationactivation.Activation
	err := callDeploymentActivation(ctx, host, call, configurationactivation.GetOperation, map[string]string{"candidateId": candidateID}, &activation)
	return activation, err
}

func publishDeploymentActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidateID string) (configurationactivation.Activation, error) {
	var activation configurationactivation.Activation
	err := callDeploymentActivation(ctx, host, call, configurationactivation.PublishOperation, map[string]string{"candidateId": candidateID}, &activation)
	return activation, err
}

func callDeploymentActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, request any, activation *configurationactivation.Activation) error {
	if host == nil {
		return errors.New("插件配置协调器缺少可信宿主")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	logicalService, routingDomain := deploymentLogicalService, "platform"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: configurationactivation.DeploymentCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}, call, payload)
	if err != nil {
		return err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil {
			return errors.New(result.Error.Message)
		}
		return errors.New("部署管理器拒绝配置激活")
	}
	if err := decodeStrict(raw, activation); err != nil {
		return err
	}
	return activation.Validate()
}

func (s *Service) beginSubmission(tenant, actor, id string, expectedRevision uint64) (pluginconfiguration.Candidate, []credentialStage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok {
		return pluginconfiguration.Candidate{}, nil, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidateDraft || candidate.Revision != expectedRevision {
		return pluginconfiguration.Candidate{}, nil, ErrConflict
	}
	previous := cloneCandidate(candidate)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidatePublishing, string(configurationactivation.StatusPendingApproval)
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[id] = candidate
	s.auditLocked(state, candidate, "configuration.application.submitting", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previous
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, nil, err
	}
	return cloneCandidate(candidate), cloneStages(state.CredentialStages[id]), nil
}

func (s *Service) checkpointExternal(tenant, id, actor string, activation configurationactivation.Activation) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok || candidate.Status != pluginconfiguration.CandidatePublishing || (candidate.ExternalRevision != 0 && candidate.ExternalRevision != activation.ServiceRevision) {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previous := cloneCandidate(candidate)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.ExternalRevision, candidate.ExternalStatus = activation.ServiceRevision, string(activation.Status)
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[id] = candidate
	s.auditLocked(state, candidate, "configuration.application.submitted", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previous
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) refreshExternalStatus(tenant, id string, activation configurationactivation.Activation) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok || candidate.Status != pluginconfiguration.CandidatePublishing || (candidate.ExternalRevision != 0 && candidate.ExternalRevision != activation.ServiceRevision) {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	if candidate.ExternalRevision == activation.ServiceRevision && candidate.ExternalStatus == string(activation.Status) {
		return cloneCandidate(candidate), nil
	}
	previous := cloneCandidate(candidate)
	candidate.ExternalRevision, candidate.ExternalStatus = activation.ServiceRevision, string(activation.Status)
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[id] = candidate
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previous
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) beginActivation(tenant, actor, id string, expectedRevision uint64, activation configurationactivation.Activation) (pluginconfiguration.Candidate, []credentialStage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok {
		return pluginconfiguration.Candidate{}, nil, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision || candidate.ExternalRevision != activation.ServiceRevision || activation.Status != configurationactivation.StatusApproved {
		return pluginconfiguration.Candidate{}, nil, ErrConflict
	}
	previous := cloneCandidate(candidate)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidateActivating, string(activation.Status)
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[id] = candidate
	s.auditLocked(state, candidate, "configuration.application.activating", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previous
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, nil, err
	}
	return cloneCandidate(candidate), cloneStages(state.CredentialStages[id]), nil
}

func (s *Service) prepareCredentialStages(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, candidateID string, stages []credentialStage) error {
	for _, binding := range stages {
		if binding.State == "Candidate" || binding.State == "Active" {
			continue
		}
		if err := callCredentials(ctx, host, call, "prepareDelegated", map[string]string{"stageId": binding.Stage.ID, "candidateId": candidateID}, nil); err != nil {
			return err
		}
		if err := s.checkpointCredentialStageState(tenant, candidateID, binding.FieldID, "Candidate"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) activateCredentialStages(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, candidateID string, stages []credentialStage) error {
	for _, binding := range stages {
		if binding.State == "Active" {
			continue
		}
		if err := callCredentials(ctx, host, call, "activateDelegated", map[string]string{"stageId": binding.Stage.ID, "candidateId": candidateID}, nil); err != nil {
			return err
		}
		if err := s.checkpointCredentialStageState(tenant, candidateID, binding.FieldID, "Active"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) checkpointCredentialStageState(tenant, candidateID, fieldID, target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	binding, ok := state.CredentialStages[candidateID][fieldID]
	if !ok {
		return ErrNotFound
	}
	previous := binding
	candidate, candidateOK := state.Candidates[candidateID]
	if !candidateOK {
		return ErrNotFound
	}
	previousCandidate := cloneCandidate(candidate)
	binding.State = target
	state.CredentialStages[candidateID][fieldID] = binding
	for index := range candidate.ManagedCredentials {
		if candidate.ManagedCredentials[index].FieldID == fieldID {
			candidate.ManagedCredentials[index].State = target
		}
	}
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[candidateID] = candidate
	if err := s.saveLocked(); err != nil {
		state.CredentialStages[candidateID][fieldID] = previous
		state.Candidates[candidateID] = previousCandidate
		return err
	}
	return nil
}

func (s *Service) completeExternalActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, actor, candidateID string, activation configurationactivation.Activation) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	stages := cloneStages(s.tenantLocked(tenant).CredentialStages[candidateID])
	s.mu.Unlock()
	switch activation.Status {
	case configurationactivation.StatusReady:
		if err := s.activateCredentialStages(ctx, host, call, tenant, candidateID, stages); err != nil {
			return pluginconfiguration.Candidate{}, err
		}
		return s.finishExternal(tenant, actor, candidateID, activation, pluginconfiguration.CandidateReady, "configuration.application.ready")
	case configurationactivation.StatusRolledBack, configurationactivation.StatusFailed:
		abortErr := abortCredentialStages(ctx, host, call, candidateID, stages)
		status := pluginconfiguration.CandidateRolledBack
		if activation.Status == configurationactivation.StatusFailed || abortErr != nil {
			status = pluginconfiguration.CandidateFailed
		}
		candidate, finishErr := s.finishExternal(tenant, actor, candidateID, activation, status, "configuration.application.rolled_back")
		return candidate, errors.Join(abortErr, finishErr)
	default:
		return s.finishExternal(tenant, actor, candidateID, activation, pluginconfiguration.CandidateActivating, "configuration.application.progress")
	}
}

func (s *Service) finishExternal(tenant, actor, candidateID string, activation configurationactivation.Activation, status pluginconfiguration.CandidateStatus, action string) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[candidateID]
	if !ok {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	previous := cloneCandidate(candidate)
	currentID, hadCurrent := state.Current[candidate.ConfigurationID]
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	candidate.Status, candidate.ExternalStatus = status, string(activation.Status)
	candidate.ExternalRevision, candidate.RollbackRevision = activation.ServiceRevision, activation.RollbackServiceRevision
	candidate.ErrorCode, candidate.ErrorMessage = activation.ErrorCode, activation.ErrorMessage
	candidate.Revision++
	candidate.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Candidates[candidateID] = candidate
	if terminal(status) {
		delete(state.Current, candidate.ConfigurationID)
	}
	s.auditLocked(state, candidate, action, actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[candidateID] = previous
		if hadCurrent {
			state.Current[candidate.ConfigurationID] = currentID
		}
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}
