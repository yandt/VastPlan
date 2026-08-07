package pluginsettings

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/configurationactivation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type interruptedCandidate struct {
	tenant    string
	candidate pluginconfiguration.Candidate
	stages    []credentialStage
}

func (s *Service) recoverInterrupted(ctx context.Context, host sdk.Host, call *contractv1.CallContext) error {
	tenant, actor, err := tenantAndActor(call)
	if err != nil {
		return err
	}
	for _, item := range s.interruptedCandidates(tenant) {
		if err := s.recoverInterruptedCandidate(ctx, host, call, actor, item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) interruptedCandidates(tenant string) []interruptedCandidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	interrupted := make([]interruptedCandidate, 0)
	for _, candidate := range state.Candidates {
		if candidate.Status == pluginconfiguration.CandidatePreparing || candidate.Status == pluginconfiguration.CandidatePublishing || candidate.Status == pluginconfiguration.CandidateActivating || candidate.Status == pluginconfiguration.CandidateRollingBack {
			interrupted = append(interrupted, interruptedCandidate{tenant: tenant, candidate: cloneCandidate(candidate), stages: cloneStages(state.CredentialStages[candidate.ID])})
		}
	}
	return interrupted
}

func (s *Service) recoverInterruptedCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, actor string, item interruptedCandidate) error {
	switch item.candidate.Status {
	case pluginconfiguration.CandidatePreparing:
		return s.recoverPreparingCandidate(ctx, host, call, actor, item)
	case pluginconfiguration.CandidateRollingBack:
		return s.recoverRollingBackCandidate(ctx, host, call, actor, item)
	case pluginconfiguration.CandidatePublishing:
		return s.recoverPublishingCandidate(ctx, host, call, actor, item)
	case pluginconfiguration.CandidateActivating:
		return s.recoverActivatingCandidate(ctx, host, call, actor, item)
	default:
		return ErrInvalid
	}
}

func (s *Service) recoverPreparingCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, actor string, item interruptedCandidate) error {
	if allCredentialFieldsCheckpointed(item.candidate) {
		_, err := s.finishPreparing(item.tenant, item.candidate.ID, actor)
		return err
	}
	abortErr := abortCredentialStages(ctx, host, call, item.candidate.ID, item.stages)
	if err := s.failPreparing(item.tenant, item.candidate.ID, actor, abortErr); err != nil {
		return errors.Join(abortErr, err)
	}
	return nil
}

func (s *Service) recoverRollingBackCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, actor string, item interruptedCandidate) error {
	if item.candidate.ApplyPath == pluginconfiguration.ApplyResourceProfile && s.resourceAbortInterrupted(item) {
		_, err := s.continueResourceAbort(ctx, host, call, item.tenant, actor, item.candidate.ID)
		return err
	}
	if item.candidate.ApplyPath == pluginconfiguration.ApplyHotService && s.hotAbortInterrupted(item) {
		_, err := s.continueHotAbort(ctx, host, call, item.tenant, actor, item.candidate.ID)
		return err
	}
	if err := abortCredentialStages(ctx, host, call, item.candidate.ID, item.stages); err != nil {
		return err
	}
	_, err := s.completeRollback(item.tenant, item.candidate.ID, actor)
	return err
}

func (s *Service) resourceAbortInterrupted(item interruptedCandidate) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	activation, exists := s.tenantLocked(item.tenant).ResourceActivations[item.candidate.ID]
	return exists && activation.Status == resourceAborting
}

func (s *Service) hotAbortInterrupted(item interruptedCandidate) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	activation, exists := s.tenantLocked(item.tenant).HotActivations[item.candidate.ID]
	return exists && activation.Status == hotAborting
}

func (s *Service) recoverPublishingCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, actor string, item interruptedCandidate) error {
	switch item.candidate.ApplyPath {
	case pluginconfiguration.ApplyApplicationDeployment:
		activation, err := s.recoverPublishing(ctx, host, call, item)
		if err != nil {
			return err
		}
		_, err = s.refreshExternalStatus(item.tenant, item.candidate.ID, activation)
		return err
	case pluginconfiguration.ApplyPlatformProfile:
		activation, err := s.recoverProfilePublishing(ctx, host, call, item)
		if err != nil {
			return err
		}
		_, err = s.refreshProfileExternal(item.tenant, actor, item.candidate.ID, activation)
		return err
	case pluginconfiguration.ApplyHotService:
		return s.recoverHotPublishing(ctx, host, call, item.tenant, actor, item.candidate.ID)
	case pluginconfiguration.ApplyResourceProfile:
		return s.recoverResourcePublishing(ctx, host, call, item.tenant, actor, item.candidate.ID)
	case pluginconfiguration.ApplyHotScoped:
		return s.recoverScopedPublishing(item.tenant, item.candidate.ID)
	default:
		return ErrInvalid
	}
}

func (s *Service) recoverActivatingCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, actor string, item interruptedCandidate) error {
	switch item.candidate.ApplyPath {
	case pluginconfiguration.ApplyResourceProfile:
		_, err := s.continueResourceActivation(ctx, host, call, item.tenant, actor, item.candidate.ID)
		return err
	case pluginconfiguration.ApplyHotService:
		_, err := s.continueHotActivation(ctx, host, call, item.tenant, actor, item.candidate.ID)
		return err
	}
	if err := s.prepareCredentialStages(ctx, host, call, item.tenant, item.candidate.ID, item.stages); err != nil {
		return err
	}
	switch item.candidate.ApplyPath {
	case pluginconfiguration.ApplyApplicationDeployment:
		activation, err := publishDeploymentActivation(ctx, host, call, item.candidate.ID)
		if err != nil {
			return err
		}
		_, err = s.completeExternalActivation(ctx, host, call, item.tenant, actor, item.candidate.ID, activation)
		return err
	case pluginconfiguration.ApplyPlatformProfile:
		activation, err := publishProfileActivation(ctx, host, call, item.candidate.ID)
		if err != nil {
			return err
		}
		_, err = s.completeProfileActivation(ctx, host, call, item.tenant, actor, item.candidate.ID, activation)
		return err
	default:
		return ErrInvalid
	}
}

func (s *Service) recoverScopedPublishing(tenant, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.tenantLocked(tenant).ScopedActivations[id]
	if !ok || (record.Status != scopedPendingApproval && record.Status != scopedApproved) {
		return ErrConflict
	}
	return nil
}

func (s *Service) recoverResourcePublishing(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, actor, id string) error {
	s.mu.Lock()
	record, ok := s.tenantLocked(tenant).ResourceActivations[id]
	s.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	if record.Status == resourcePreparing {
		_, err := s.resumeResourcePreparation(ctx, host, call, tenant, actor, id)
		return err
	}
	if record.Status != resourcePendingApproval && record.Status != resourceApproved {
		return ErrConflict
	}
	observation, err := callResourceObservation(ctx, host, call, record.Target, configurationresourcev1.OperationStatus, configurationresourcev1.StatusRequest{
		CollectionID: record.Prepare.CollectionID, ResourceID: record.Prepare.ResourceID, CandidateID: id, RequestDigest: record.RequestDigest,
	})
	if err != nil {
		return err
	}
	if observation.Candidate == nil || observation.Candidate.Status != configurationresourcev1.StatusPrepared {
		return errors.New("configuration.resource.v1 待审批 Candidate 状态漂移")
	}
	return nil
}

func (s *Service) recoverHotPublishing(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, actor, id string) error {
	s.mu.Lock()
	record, ok := s.tenantLocked(tenant).HotActivations[id]
	s.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	if record.Status == hotPreparing {
		_, err := s.resumeHotPreparation(ctx, host, call, tenant, actor, id)
		return err
	}
	if record.Status != hotPendingApproval && record.Status != hotApproved {
		return ErrConflict
	}
	observation, err := getHotControllerStatus(ctx, host, call, record.Target, configurationv1.StatusRequest{
		ConfigurationID: record.Prepare.ConfigurationID, CandidateID: record.Prepare.CandidateID, RequestDigest: record.RequestDigest,
	})
	if err != nil {
		return err
	}
	if observation.Candidate == nil || observation.Candidate.Status != configurationv1.StatusPrepared {
		return errors.New("configuration.v1 待审批 Candidate 状态漂移")
	}
	return nil
}

func (s *Service) recoverProfilePublishing(ctx context.Context, host sdk.Host, call *contractv1.CallContext, item interruptedCandidate) (platformprofileactivation.Activation, error) {
	if item.candidate.ExternalRevision != 0 {
		return getProfileActivation(ctx, host, call, item.candidate.ID)
	}
	definition, err := s.currentDefinition(ctx, host, call, item.candidate)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	if definition.ApplyPath != pluginconfiguration.ApplyPlatformProfile {
		return platformprofileactivation.Activation{}, ErrInvalid
	}
	return createProfileActivation(ctx, host, call, definition, item.candidate, item.stages)
}

func (s *Service) refreshProfileExternal(tenant, actor, id string, activation platformprofileactivation.Activation) (pluginconfiguration.Candidate, error) {
	return s.checkpointProfileExternal(tenant, actor, id, activation, "configuration.profile.recovered")
}

func (s *Service) recoverPublishing(ctx context.Context, host sdk.Host, call *contractv1.CallContext, item interruptedCandidate) (configurationactivation.Activation, error) {
	if item.candidate.ExternalRevision != 0 {
		return getDeploymentActivation(ctx, host, call, item.candidate.ID)
	}
	definition, err := s.currentDefinition(ctx, host, call, item.candidate)
	if err != nil {
		return configurationactivation.Activation{}, err
	}
	if definition.ApplyPath != pluginconfiguration.ApplyApplicationDeployment {
		return configurationactivation.Activation{}, ErrInvalid
	}
	return createDeploymentActivation(ctx, host, call, definition, item.candidate, item.stages)
}

func allCredentialFieldsCheckpointed(candidate pluginconfiguration.Candidate) bool {
	for _, field := range candidate.ManagedCredentials {
		if field.State == "Pending" {
			return false
		}
	}
	return len(candidate.ManagedCredentials) > 0
}
