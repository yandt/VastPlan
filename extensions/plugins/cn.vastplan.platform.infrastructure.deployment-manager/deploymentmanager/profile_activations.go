package deploymentmanager

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var errProfileActivation = errors.New("Platform Profile 配置激活失败")

func (s *Service) CreateProfileConfigurationActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request platformprofileactivation.CreateActivationRequest) (platformprofileactivation.Activation, error) {
	if err := request.Validate(); err != nil || !profileActivationCaller(call) {
		return platformprofileactivation.Activation{}, errInvalid
	}
	tenant, err := callTenant(call)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	principal, err := actor(call)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	if existing, ok := state.ProfileActivations[request.CandidateID]; ok {
		if profileActivationSubmissionHash(existing.Request) != profileActivationSubmissionHash(request) {
			s.mu.Unlock()
			return platformprofileactivation.Activation{}, errServiceState
		}
		s.mu.Unlock()
		if existing.Status == platformprofileactivation.ActivationPreparing {
			return s.resumeProfilePreparation(ctx, host, call, tenant, existing.CandidateID)
		}
		return publicProfileActivation(existing), nil
	}
	active, _, err := activePlatformConfigurationDefinition(state, request)
	if err != nil {
		s.mu.Unlock()
		return platformprofileactivation.Activation{}, err
	}
	if profileActivationLocksDeployment(state, active.Deployment) {
		s.mu.Unlock()
		return platformprofileactivation.Activation{}, errServiceState
	}
	revision := state.NextRevision + 1
	composition, err := normalizeServiceComposition(active.Composition, tenant, revision)
	if err != nil {
		s.mu.Unlock()
		return platformprofileactivation.Activation{}, errInvalid
	}
	prepare := platformprofileactivation.PrepareRequest{
		CandidateID: request.CandidateID, ConfigurationID: request.ConfigurationID,
		ConfigCatalogDigest: request.ConfigCatalogDigest, SchemaDigest: request.SchemaDigest, ArtifactSHA256: request.ArtifactSHA256,
		Values: append(json.RawMessage(nil), request.Values...), Credentials: cloneJSON(request.Credentials),
		Composition: composition, DeploymentRevision: revision,
	}
	requestDigest, err := platformprofileactivation.DigestPrepareRequest(prepare)
	if err != nil {
		s.mu.Unlock()
		return platformprofileactivation.Activation{}, errInvalid
	}
	now := s.now().Format(time.RFC3339Nano)
	record := profileActivationRecord{
		Activation: platformprofileactivation.Activation{
			CandidateID: request.CandidateID, ConfigurationID: request.ConfigurationID, Deployment: active.Deployment,
			DeploymentRevision: revision, PreviousServiceRevision: active.ID,
			Status: platformprofileactivation.ActivationPreparing, RequestedBy: principal,
		},
		Request: request, Prepare: prepare, RequestDigest: requestDigest, CreatedAt: now, UpdatedAt: now,
	}
	state.NextRevision = revision
	state.ProfileActivations[record.CandidateID] = record
	if err := s.saveLocked(); err != nil {
		delete(state.ProfileActivations, record.CandidateID)
		state.NextRevision--
		s.mu.Unlock()
		return platformprofileactivation.Activation{}, err
	}
	s.mu.Unlock()
	return s.resumeProfilePreparation(ctx, host, call, tenant, record.CandidateID)
}

func (s *Service) resumeProfilePreparation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant, candidateID string) (platformprofileactivation.Activation, error) {
	s.mu.Lock()
	record, ok := s.tenantLocked(tenant).ProfileActivations[candidateID]
	s.mu.Unlock()
	if !ok {
		return platformprofileactivation.Activation{}, errNotFound
	}
	if record.Status != platformprofileactivation.ActivationPreparing {
		return publicProfileActivation(record), nil
	}
	var prepared platformprofileactivation.PrepareResult
	if err := callKernelProfileActivation(ctx, host, call, platformprofileactivation.KernelPrepareService, record.Prepare, &prepared); err != nil {
		return platformprofileactivation.Activation{}, err
	}
	if prepared.Candidate.CandidateID != record.CandidateID || prepared.Candidate.RequestDigest != record.RequestDigest ||
		prepared.Candidate.ConfigurationID != record.ConfigurationID || prepared.Candidate.Deployment != record.Deployment ||
		prepared.Candidate.Status != platformprofileactivation.StatusPrepared || prepared.Preview.Digest == "" {
		return platformprofileactivation.Activation{}, errProfileActivation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	current, ok := state.ProfileActivations[candidateID]
	if !ok || current.Status != platformprofileactivation.ActivationPreparing || current.RequestDigest != record.RequestDigest {
		return platformprofileactivation.Activation{}, errServiceState
	}
	previous := cloneProfileActivation(current)
	current.Status = platformprofileactivation.ActivationPendingApproval
	current.CandidateStatus, current.Preview = prepared.Candidate.Status, prepared.Preview
	current.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.ProfileActivations[candidateID] = current
	if err := s.saveLocked(); err != nil {
		state.ProfileActivations[candidateID] = previous
		return platformprofileactivation.Activation{}, err
	}
	return publicProfileActivation(current), nil
}

func (s *Service) GetProfileConfigurationActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request platformprofileactivation.ActivationLookup) (platformprofileactivation.Activation, error) {
	if err := request.Validate(); err != nil || !profileActivationCaller(call) {
		return platformprofileactivation.Activation{}, errInvalid
	}
	tenant, err := callTenant(call)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	s.mu.Lock()
	record, ok := s.tenantLocked(tenant).ProfileActivations[request.CandidateID]
	s.mu.Unlock()
	if !ok {
		return platformprofileactivation.Activation{}, errNotFound
	}
	if record.Status == platformprofileactivation.ActivationPreparing {
		return s.resumeProfilePreparation(ctx, host, call, tenant, request.CandidateID)
	}
	return publicProfileActivation(record), nil
}

func (s *Service) ApproveProfileConfigurationActivation(call *contractv1.CallContext, request platformprofileactivation.ActivationLookup) (platformprofileactivation.Activation, error) {
	if err := request.Validate(); err != nil || !profileActivationCaller(call) {
		return platformprofileactivation.Activation{}, errInvalid
	}
	tenant, err := callTenant(call)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	approver, err := actor(call)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	record, ok := state.ProfileActivations[request.CandidateID]
	if !ok {
		return platformprofileactivation.Activation{}, errNotFound
	}
	if record.Status != platformprofileactivation.ActivationPendingApproval {
		return platformprofileactivation.Activation{}, errServiceState
	}
	if record.RequestedBy == approver {
		return platformprofileactivation.Activation{}, errSeparation
	}
	previous := cloneProfileActivation(record)
	record.Status, record.ApprovedBy = platformprofileactivation.ActivationApproved, approver
	record.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.ProfileActivations[record.CandidateID] = record
	if err := s.saveLocked(); err != nil {
		state.ProfileActivations[record.CandidateID] = previous
		return platformprofileactivation.Activation{}, err
	}
	return publicProfileActivation(record), nil
}

func (s *Service) AbortProfileConfigurationActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request platformprofileactivation.ActivationLookup) (platformprofileactivation.Activation, error) {
	if err := request.Validate(); err != nil || !profileActivationCaller(call) {
		return platformprofileactivation.Activation{}, errInvalid
	}
	tenant, err := callTenant(call)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	s.mu.Lock()
	record, ok := s.tenantLocked(tenant).ProfileActivations[request.CandidateID]
	s.mu.Unlock()
	if !ok {
		return platformprofileactivation.Activation{}, errNotFound
	}
	if record.Status == platformprofileactivation.ActivationAborted {
		return publicProfileActivation(record), nil
	}
	if record.Status != platformprofileactivation.ActivationPendingApproval && record.Status != platformprofileactivation.ActivationApproved {
		return platformprofileactivation.Activation{}, errServiceState
	}
	key := platformprofileactivation.CandidateRequest{CandidateID: record.CandidateID, RequestDigest: record.RequestDigest}
	var candidate platformprofileactivation.Candidate
	if err := callKernelProfileActivation(ctx, host, call, platformprofileactivation.KernelAbortService, key, &candidate); err != nil || candidate.Status != platformprofileactivation.StatusAborted {
		return platformprofileactivation.Activation{}, errProfileActivation
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	current := state.ProfileActivations[record.CandidateID]
	if current.Status != record.Status {
		return platformprofileactivation.Activation{}, errServiceState
	}
	current.Status, current.CandidateStatus = platformprofileactivation.ActivationAborted, candidate.Status
	current.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.ProfileActivations[current.CandidateID] = current
	if err := s.saveLocked(); err != nil {
		return platformprofileactivation.Activation{}, err
	}
	return publicProfileActivation(current), nil
}

func (s *Service) PublishProfileConfigurationActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request platformprofileactivation.ActivationLookup) (platformprofileactivation.Activation, error) {
	if err := request.Validate(); err != nil || !profileActivationCaller(call) {
		return platformprofileactivation.Activation{}, errInvalid
	}
	tenant, err := callTenant(call)
	if err != nil {
		return platformprofileactivation.Activation{}, err
	}
	for {
		s.mu.Lock()
		record, ok := s.tenantLocked(tenant).ProfileActivations[request.CandidateID]
		s.mu.Unlock()
		if !ok {
			return platformprofileactivation.Activation{}, errNotFound
		}
		switch record.Status {
		case platformprofileactivation.ActivationReady, platformprofileactivation.ActivationRolledBack:
			return publicProfileActivation(record), nil
		case platformprofileactivation.ActivationApproved:
			if err := s.activateProfileCatalog(ctx, host, call, tenant, record); err != nil {
				return platformprofileactivation.Activation{}, err
			}
		case platformprofileactivation.ActivationCatalogActive:
			if err := s.publishProfileDeployment(ctx, host, call, tenant, record); err != nil {
				return s.rollbackProfileActivation(ctx, host, call, tenant, record.CandidateID, err)
			}
		case platformprofileactivation.ActivationPublishing:
			waitCtx := ctx
			if _, hasDeadline := ctx.Deadline(); !hasDeadline {
				var cancel context.CancelFunc
				waitCtx, cancel = context.WithTimeout(ctx, s.releaseTimeout)
				defer cancel()
			}
			if err := s.waitForServiceReadiness(waitCtx, host, call, record.Deployment, record.DeploymentRevision); err != nil {
				return s.rollbackProfileActivation(ctx, host, call, tenant, record.CandidateID, err)
			}
			if err := s.finalizeProfileActivation(ctx, host, call, tenant, record); err != nil {
				return platformprofileactivation.Activation{}, err
			}
		case platformprofileactivation.ActivationRollingBack:
			return s.rollbackProfileActivation(ctx, host, call, tenant, record.CandidateID, errors.New(record.ErrorMessage))
		default:
			return platformprofileactivation.Activation{}, errServiceState
		}
	}
}

func profileActivationCaller(call *contractv1.CallContext) bool {
	return call != nil && call.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_PLUGIN && call.GetCaller().GetId() == pluginconfiguration.PluginSettingsID
}

func (s *Service) activateProfileCatalog(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant string, record profileActivationRecord) error {
	key := platformprofileactivation.CandidateRequest{CandidateID: record.CandidateID, RequestDigest: record.RequestDigest}
	var candidate platformprofileactivation.Candidate
	if err := callKernelProfileActivation(ctx, host, call, platformprofileactivation.KernelActivateService, key, &candidate); err != nil || candidate.Status != platformprofileactivation.StatusActivated {
		return errProfileActivation
	}
	return s.checkpointProfileStatus(tenant, record.CandidateID, record.Status, platformprofileactivation.ActivationCatalogActive, candidate.Status, "", "")
}

func (s *Service) publishProfileDeployment(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant string, record profileActivationRecord) error {
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	previous, err := serviceRevisionByID(state, record.PreviousServiceRevision)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := protectDeploymentTransition(ctx, host, call, record.Deployment, record.DeploymentRevision*2-1, previous.ArtifactReferences, record.Preview.ArtifactReferences); err != nil {
		return err
	}
	publish := platformprofileactivation.PublishRequest{Prepare: record.Prepare, RequestDigest: record.RequestDigest, ExpectedDigest: record.Preview.Digest}
	var result deploymentpublication.Result
	if err := callKernelProfileActivation(ctx, host, call, platformprofileactivation.KernelPublishService, publish, &result); err != nil {
		return err
	}
	if result.Digest != record.Preview.Digest || result.Deployment.Revision != record.DeploymentRevision {
		return errProfileActivation
	}
	publisher := actorOrUnknown(call)
	s.mu.Lock()
	state = s.tenantLocked(tenant)
	current := state.ProfileActivations[record.CandidateID]
	if current.Status != platformprofileactivation.ActivationCatalogActive {
		s.mu.Unlock()
		return errServiceState
	}
	oldRevisions := cloneServiceRevisions(state.Revisions)
	oldAuditLength, oldNextAudit := len(state.ServiceAudit), state.NextAudit
	serviceRevision := platformadminapi.ServiceRevision{
		ID: record.DeploymentRevision, Deployment: record.Deployment, Status: platformadminapi.ServicePublished, Active: true,
		Composition: record.Prepare.Composition, Preview: result.Deployment, PreviewDigest: result.Digest,
		ArtifactReferences: result.ArtifactReferences, ConfigurationCatalog: result.ConfigurationCatalog,
		ConfigurationID: record.ConfigurationID, PreviousServiceRevision: record.PreviousServiceRevision,
		KVRevision: result.KVRevision, ReferencePending: true, SubmittedBy: record.RequestedBy, ApprovedBy: record.ApprovedBy, PublishedBy: publisher,
		CreatedAt: record.CreatedAt, UpdatedAt: s.now().Format(time.RFC3339Nano),
	}
	for index := range state.Revisions {
		if state.Revisions[index].Deployment == record.Deployment {
			state.Revisions[index].Active = false
		}
	}
	state.Revisions = append(state.Revisions, serviceRevision)
	current.Status, current.CandidateStatus, current.Preview = platformprofileactivation.ActivationPublishing, platformprofileactivation.StatusActivated, result
	current.UpdatedAt = serviceRevision.UpdatedAt
	state.ProfileActivations[current.CandidateID] = current
	s.auditServiceLocked(state, serviceRevision, "service.profile_configuration.published", publisher)
	if err := s.saveLocked(); err != nil {
		state.Revisions = oldRevisions
		state.ProfileActivations[current.CandidateID] = record
		state.ServiceAudit = state.ServiceAudit[:oldAuditLength]
		state.NextAudit = oldNextAudit
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	if err := publishDeploymentReferences(ctx, host, call, record.Deployment, record.DeploymentRevision, result.ArtifactReferences, previous.ArtifactReferences); err == nil {
		s.markServiceReferencesSynced(tenant, record.DeploymentRevision)
	}
	return nil
}

func (s *Service) finalizeProfileActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant string, record profileActivationRecord) error {
	key := platformprofileactivation.CandidateRequest{CandidateID: record.CandidateID, RequestDigest: record.RequestDigest}
	var candidate platformprofileactivation.Candidate
	if err := callKernelProfileActivation(ctx, host, call, platformprofileactivation.KernelFinalizeService, key, &candidate); err != nil || candidate.Status != platformprofileactivation.StatusFinalized {
		return errProfileActivation
	}
	return s.checkpointProfileStatus(tenant, record.CandidateID, record.Status, platformprofileactivation.ActivationReady, candidate.Status, "", "")
}

func (s *Service) checkpointProfileStatus(tenant, candidateID string, from, to platformprofileactivation.ActivationStatus, candidateStatus platformprofileactivation.Status, errorCode, errorMessage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	record, ok := state.ProfileActivations[candidateID]
	if !ok || record.Status != from {
		return errServiceState
	}
	previous := cloneProfileActivation(record)
	record.Status, record.CandidateStatus = to, candidateStatus
	record.ErrorCode, record.ErrorMessage = errorCode, errorMessage
	record.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.ProfileActivations[candidateID] = record
	if err := s.saveLocked(); err != nil {
		state.ProfileActivations[candidateID] = previous
		return err
	}
	return nil
}

func activePlatformConfigurationDefinition(state *tenantState, request platformprofileactivation.CreateActivationRequest) (platformadminapi.ServiceRevision, pluginconfiguration.Definition, error) {
	var matchedRevision platformadminapi.ServiceRevision
	var matchedDefinition pluginconfiguration.Definition
	for _, revision := range state.Revisions {
		if !revision.Active || revision.Status != platformadminapi.ServicePublished || revision.ConfigurationCatalog.Digest != request.ConfigCatalogDigest {
			continue
		}
		for _, definition := range revision.ConfigurationCatalog.Items {
			if definition.ID != request.ConfigurationID {
				continue
			}
			if matchedRevision.ID != 0 || definition.ApplyPath != pluginconfiguration.ApplyPlatformProfile ||
				definition.SchemaDigest != request.SchemaDigest || definition.Artifact.SHA256 != request.ArtifactSHA256 {
				return platformadminapi.ServiceRevision{}, pluginconfiguration.Definition{}, errServiceState
			}
			matchedRevision, matchedDefinition = cloneServiceRevision(revision), definition
		}
	}
	if matchedRevision.ID == 0 {
		return platformadminapi.ServiceRevision{}, pluginconfiguration.Definition{}, errNotFound
	}
	if err := pluginconfiguration.ValidateValues(matchedDefinition, request.Values); err != nil {
		return platformadminapi.ServiceRevision{}, pluginconfiguration.Definition{}, errInvalid
	}
	return matchedRevision, matchedDefinition, nil
}

func profileActivationSubmissionHash(request platformprofileactivation.CreateActivationRequest) string {
	raw, _ := json.Marshal(request)
	digest := sha256.Sum256(raw)
	return fmt.Sprintf("%x", digest[:])
}

func callKernelProfileActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, capability string, request, response any) error {
	if host == nil {
		return errProfileActivation
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	operation := "execute"
	result, payload, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: capability, Operation: &operation}, call, raw)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return fmt.Errorf("%w: %s", errProfileActivation, capability)
	}
	if response == nil {
		return nil
	}
	if err := json.Unmarshal(payload, response); err != nil {
		return fmt.Errorf("%w: %s response", errProfileActivation, capability)
	}
	return nil
}

func serviceRevisionByID(state *tenantState, id uint64) (platformadminapi.ServiceRevision, error) {
	index, err := serviceRevisionIndex(state, id)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	return cloneServiceRevision(state.Revisions[index]), nil
}

func (s *Service) markServiceReferencesSynced(tenant string, id uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	index, err := serviceRevisionIndex(state, id)
	if err != nil || !state.Revisions[index].ReferencePending {
		return
	}
	previous := cloneServiceRevision(state.Revisions[index])
	state.Revisions[index].ReferencePending = false
	state.Revisions[index].UpdatedAt = s.now().Format(time.RFC3339Nano)
	if err := s.saveLocked(); err != nil {
		state.Revisions[index] = previous
	}
}
