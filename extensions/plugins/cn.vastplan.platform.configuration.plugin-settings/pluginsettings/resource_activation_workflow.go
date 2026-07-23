package pluginsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) SubmitResourceDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	draft, err := s.candidateSnapshot(tenant, id, expectedRevision, pluginconfiguration.CandidateDraft)
	if err != nil || draft.ApplyPath != pluginconfiguration.ApplyResourceProfile {
		return pluginconfiguration.Candidate{}, errOrInvalid(err)
	}
	definition, collection, err := s.resourceDefinition(ctx, host, call, draft.ConfigurationID, draft.ResourceCollectionID, draft.CatalogDigest)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if collection.SchemaDigest != draft.SchemaDigest || definition.Artifact.SHA256 != draft.ArtifactSHA256 || definition.ResourceController == nil {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	expectedActive, err := s.verifyResourceDraftBaseline(ctx, host, call, definition, collection, draft)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	s.mu.Lock()
	stages := cloneStages(s.tenantLocked(tenant).CredentialStages[id])
	s.mu.Unlock()
	prepare := configurationresourcev1.PrepareRequest{
		CandidateID: draft.ID, ConfigurationID: draft.ConfigurationID, CollectionID: draft.ResourceCollectionID,
		ResourceID: draft.ResourceID, Action: configurationresourcev1.Action(draft.ResourceAction), CatalogDigest: draft.CatalogDigest,
		SchemaDigest: draft.SchemaDigest, ArtifactSHA256: draft.ArtifactSHA256, ExpectedActive: expectedActive,
		Values: append(json.RawMessage(nil), draft.Values...), ManagedCredentials: resourceCredentialRefs(stages),
	}
	requestDigest, err := configurationresourcev1.DigestPrepareRequest(prepare)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	candidate, record, stages, err := s.beginResourceSubmission(tenant, actor, id, expectedRevision, *definition.ResourceController, prepare, requestDigest)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if err := s.prepareCredentialStages(ctx, host, call, tenant, candidate.ID, stages); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	observation, err := callResourceObservation(ctx, host, call, record.Target, configurationresourcev1.OperationPrepare, record.Prepare)
	if err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	if err := validateResourcePrepared(record, observation); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.checkpointResourcePendingApproval(tenant, actor, candidate.ID, observation)
}

func (s *Service) ApproveResourceCandidate(call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.ResourceActivations[id]
	if !ok || !recordOK {
		return pluginconfiguration.Candidate{}, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidatePublishing || candidate.Revision != expectedRevision || record.Status != resourcePendingApproval || record.SubmittedBy == actor {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneResourceActivation(record)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	record.Status, record.ApprovedBy, record.UpdatedAt = resourceApproved, actor, s.now().Format(time.RFC3339Nano)
	candidate.ExternalStatus, candidate.Revision, candidate.UpdatedAt = string(resourceApproved), candidate.Revision+1, record.UpdatedAt
	state.ResourceActivations[id], state.Candidates[id] = record, candidate
	s.auditLocked(state, candidate, "configuration.resource.approved", actor)
	if err := s.saveLocked(); err != nil {
		state.ResourceActivations[id], state.Candidates[id] = previousRecord, previousCandidate
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func (s *Service) ActivateResourceCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	if _, err := s.beginResourceActivation(tenant, actor, id, expectedRevision); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.continueResourceActivation(ctx, host, call, tenant, actor, id)
}

func (s *Service) AbortResourceCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, expectedRevision uint64) (pluginconfiguration.Candidate, error) {
	tenant, actor, err := tenantAndActor(call)
	if err != nil || id == "" || expectedRevision == 0 {
		return pluginconfiguration.Candidate{}, ErrInvalid
	}
	if _, err := s.beginResourceAbort(tenant, actor, id, expectedRevision); err != nil {
		return pluginconfiguration.Candidate{}, err
	}
	return s.continueResourceAbort(ctx, host, call, tenant, actor, id)
}

func (s *Service) verifyResourceDraftBaseline(ctx context.Context, host sdk.Host, call *contractv1.CallContext, definition pluginconfiguration.Definition, collection pluginconfiguration.ResourceCollection, draft pluginconfiguration.Candidate) (*configurationresourcev1.ActiveReference, error) {
	action := configurationresourcev1.Action(draft.ResourceAction)
	if action == configurationresourcev1.ActionCreate {
		if draft.ExternalRevision != 0 || draft.ExternalDigest != "" {
			return nil, ErrConflict
		}
		return nil, nil
	}
	response, err := getResourceItem(ctx, host, call, *definition.ResourceController, configurationresourcev1.GetRequest{CollectionID: collection.ID, ResourceID: draft.ResourceID})
	if err != nil {
		return nil, err
	}
	if response.Item.Active.Revision != draft.ExternalRevision || response.Item.Active.Digest != draft.ExternalDigest {
		return nil, fmt.Errorf("%w: Profile Active 已在草稿创建后变化", ErrConflict)
	}
	active := response.Item.Active
	return &active, nil
}

func (s *Service) beginResourceSubmission(tenant, actor, id string, expectedRevision uint64, target pluginconfiguration.ControllerTarget, prepare configurationresourcev1.PrepareRequest, requestDigest string) (pluginconfiguration.Candidate, resourceActivationRecord, []credentialStage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	if !ok {
		return pluginconfiguration.Candidate{}, resourceActivationRecord{}, nil, ErrNotFound
	}
	if candidate.Status != pluginconfiguration.CandidateDraft || candidate.Revision != expectedRevision {
		return pluginconfiguration.Candidate{}, resourceActivationRecord{}, nil, ErrConflict
	}
	previousCandidate := cloneCandidate(candidate)
	previousRecord, hadRecord := state.ResourceActivations[id]
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record := resourceActivationRecord{Target: target, Prepare: prepare, RequestDigest: requestDigest, Status: resourcePreparing, SubmittedBy: actor, CreatedAt: now, UpdatedAt: now}
	candidate.Status, candidate.ExternalStatus = pluginconfiguration.CandidatePublishing, string(resourcePreparing)
	candidate.Revision, candidate.UpdatedAt = candidate.Revision+1, now
	state.Candidates[id], state.ResourceActivations[id] = candidate, record
	s.auditLocked(state, candidate, "configuration.resource.preparing", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id] = previousCandidate
		if hadRecord {
			state.ResourceActivations[id] = previousRecord
		} else {
			delete(state.ResourceActivations, id)
		}
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, resourceActivationRecord{}, nil, err
	}
	return cloneCandidate(candidate), cloneResourceActivation(record), cloneStages(state.CredentialStages[id]), nil
}

func (s *Service) checkpointResourcePendingApproval(tenant, actor, id string, observation configurationresourcev1.Observation) (pluginconfiguration.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	candidate, ok := state.Candidates[id]
	record, recordOK := state.ResourceActivations[id]
	if !ok || !recordOK || candidate.Status != pluginconfiguration.CandidatePublishing || record.Status != resourcePreparing {
		return pluginconfiguration.Candidate{}, ErrConflict
	}
	previousCandidate, previousRecord := cloneCandidate(candidate), cloneResourceActivation(record)
	auditLength, nextAudit := len(state.Audit), state.NextAudit
	now := s.now().Format(time.RFC3339Nano)
	record.Status, record.UpdatedAt = resourcePendingApproval, now
	candidate.ExternalStatus, candidate.Revision, candidate.UpdatedAt = string(configurationresourcev1.StatusPrepared), candidate.Revision+1, now
	state.Candidates[id], state.ResourceActivations[id] = candidate, record
	s.auditLocked(state, candidate, "configuration.resource.pending-approval", actor)
	if err := s.saveLocked(); err != nil {
		state.Candidates[id], state.ResourceActivations[id] = previousCandidate, previousRecord
		state.Audit, state.NextAudit = state.Audit[:auditLength], nextAudit
		return pluginconfiguration.Candidate{}, err
	}
	return cloneCandidate(candidate), nil
}

func validateResourcePrepared(record resourceActivationRecord, observation configurationresourcev1.Observation) error {
	if observation.CollectionID != record.Prepare.CollectionID || observation.ResourceID != record.Prepare.ResourceID || observation.Candidate == nil ||
		observation.Candidate.CandidateID != record.Prepare.CandidateID || observation.Candidate.RequestDigest != record.RequestDigest ||
		observation.Candidate.Action != record.Prepare.Action || observation.Candidate.Status != configurationresourcev1.StatusPrepared || !observation.Candidate.Ready {
		return fmt.Errorf("configuration.resource.v1 prepare 响应未绑定精确候选")
	}
	if record.Prepare.ExpectedActive == nil {
		if observation.Active != nil {
			return fmt.Errorf("configuration.resource.v1 create 响应伪造 Active")
		}
	} else if observation.Active == nil || *observation.Active != *record.Prepare.ExpectedActive {
		return fmt.Errorf("configuration.resource.v1 prepare Active CAS 不一致")
	}
	return nil
}

func errOrInvalid(err error) error {
	if err != nil {
		return err
	}
	return ErrInvalid
}
