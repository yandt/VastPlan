package deploymentmanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var (
	errInstallationCandidateConflict = errors.New("目标服务已有未完成的插件安装候选")
	errInstallationNoop              = errors.New("插件安装候选没有产生任何意图或制品变化")
)

func (s *Service) CreatePluginInstallationCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, source plugininstallation.Source, input plugininstallation.PreviewRequest) (plugininstallation.Candidate, error) {
	planned, err := s.planPluginInstallation(ctx, host, call, source, input)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	if planned.preview.Impact.Noop {
		return plugininstallation.Candidate{}, errInstallationNoop
	}
	requestedBy, err := actor(call)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	id, err := randomInstallationCandidateID()
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	tenant, err := callTenant(call)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	if state.NextRevision+1 != planned.candidate.Revision {
		return plugininstallation.Candidate{}, errVersionConflict
	}
	active, err := activeServiceRevision(state, planned.request.Target.Deployment)
	if err != nil || active.ID != planned.active.ID {
		return plugininstallation.Candidate{}, errVersionConflict
	}
	if installationCandidateInProgress(state, planned.request.Target.Deployment) {
		return plugininstallation.Candidate{}, errInstallationCandidateConflict
	}

	now := s.now().Format(time.RFC3339Nano)
	revision := platformadminapi.ServiceRevision{
		ID: planned.candidate.Revision, Deployment: planned.candidate.Metadata.Name, Status: platformadminapi.ServiceDraft,
		Intent: &planned.candidate, ConfigurationSnapshot: clonePlanningSnapshot(active.ConfigurationSnapshot),
		CreatedAt: now, UpdatedAt: now,
	}
	applyIntentPlan(&revision, planned.plan)
	record := installationCandidateRecord{
		ID: id, Source: source, Request: planned.request, Preview: installationPreviewSummary(planned.preview),
		ServiceRevisionID: revision.ID, PreviousServiceRevisionID: active.ID,
		RequestedBy: requestedBy, CreatedAt: now, UpdatedAt: now,
		Migration: initialMigrationState(planned.preview.Impact.Schema, now),
	}

	oldNextRevision, oldRevisionLength := state.NextRevision, len(state.Revisions)
	oldAuditLength, oldNextAudit := len(state.ServiceAudit), state.NextAudit
	state.NextRevision = revision.ID
	state.Revisions = append(state.Revisions, revision)
	state.InstallationCandidates[id] = record
	s.auditServiceLocked(state, revision, "service.installation_candidate.created", requestedBy)
	if err := s.saveLocked(); err != nil {
		state.NextRevision = oldNextRevision
		state.Revisions = state.Revisions[:oldRevisionLength]
		state.ServiceAudit, state.NextAudit = state.ServiceAudit[:oldAuditLength], oldNextAudit
		delete(state.InstallationCandidates, id)
		return plugininstallation.Candidate{}, err
	}
	return projectInstallationCandidate(state, record)
}

func installationPreviewSummary(preview plugininstallation.Preview) plugininstallation.Preview {
	preview = cloneJSON(preview)
	// The linked ServiceRevision already owns the exact ArtifactLock. Keeping a
	// second full copy in the tenant aggregate would consume the 1 MiB Shared
	// State budget without adding a distinct recovery fact.
	preview.ArtifactLock = nil
	return preview
}

func (s *Service) ListPluginInstallationCandidates(call *contractv1.CallContext) ([]plugininstallation.Candidate, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return sortedInstallationCandidates(s.tenantLocked(tenant))
}

func (s *Service) ListSelfServicePluginInstallationCandidates(call *contractv1.CallContext, target plugininstallation.Target) ([]plugininstallation.Candidate, error) {
	if err := validateInstallationTarget(target); err != nil {
		return nil, err
	}
	items, err := s.ListPluginInstallationCandidates(call)
	if err != nil {
		return nil, err
	}
	filtered := make([]plugininstallation.Candidate, 0, len(items))
	for _, candidate := range items {
		if candidate.Source == plugininstallation.SourceSelfService && candidate.Preview.Target == target {
			filtered = append(filtered, candidate)
		}
	}
	return filtered, nil
}

func (s *Service) GetSelfServicePluginInstallationCandidate(call *contractv1.CallContext, id string, target plugininstallation.Target) (plugininstallation.Candidate, error) {
	candidate, err := s.GetPluginInstallationCandidate(call, id)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	if candidate.Source != plugininstallation.SourceSelfService || candidate.Preview.Target != target {
		return plugininstallation.Candidate{}, plugininstallation.ErrTargetScopeMismatch
	}
	return candidate, nil
}

func (s *Service) GetPluginInstallationCandidate(call *contractv1.CallContext, id string) (plugininstallation.Candidate, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	record, ok := state.InstallationCandidates[id]
	if !ok {
		return plugininstallation.Candidate{}, errNotFound
	}
	return projectInstallationCandidate(state, record)
}

func (s *Service) SubmitPluginInstallationCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string) (plugininstallation.Candidate, error) {
	candidate, err := s.GetPluginInstallationCandidate(call, id)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	if candidate.Status == plugininstallation.CandidatePlanned {
		if _, err := s.submitServiceDraftForOwner(ctx, host, call, candidate.ServiceRevisionID, revisionOwnerInstallation); err != nil {
			return plugininstallation.Candidate{}, err
		}
	} else if candidate.Status != plugininstallation.CandidatePendingApproval || candidate.Preview.Impact.RequiresApproval {
		return plugininstallation.Candidate{}, errServiceState
	}
	if !candidate.Preview.Impact.RequiresApproval {
		if err := s.approveInstallationByPolicy(call, id); err != nil {
			return plugininstallation.Candidate{}, err
		}
	}
	return s.GetPluginInstallationCandidate(call, id)
}

func (s *Service) SubmitSelfServicePluginInstallationCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, target plugininstallation.Target) (plugininstallation.Candidate, error) {
	if _, err := s.GetSelfServicePluginInstallationCandidate(call, id, target); err != nil {
		return plugininstallation.Candidate{}, err
	}
	return s.SubmitPluginInstallationCandidate(ctx, host, call, id)
}

func (s *Service) ApprovePluginInstallationCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string) (plugininstallation.Candidate, error) {
	return s.ApprovePluginInstallationCandidateWithEvidence(ctx, host, call, id, nil)
}

func (s *Service) ApprovePluginInstallationCandidateWithEvidence(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, evidence map[string]json.RawMessage) (plugininstallation.Candidate, error) {
	candidate, err := s.GetPluginInstallationCandidate(call, id)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	if s.approvalBinding != nil {
		decision, evaluateErr := s.evaluateInstallationApproval(ctx, host, call, candidate.Preview, candidate.SubmittedBy, evidence)
		if evaluateErr != nil {
			return plugininstallation.Candidate{}, evaluateErr
		}
		if decision == nil || decision.Status != approvalv2.DecisionAllowed {
			return plugininstallation.Candidate{}, errInstallationApprovalRequired
		}
	}
	if err := s.attachInstallationBackupEvidence(call, id, candidate.Preview.Impact.Schema, evidence); err != nil {
		return plugininstallation.Candidate{}, err
	}
	revisionID, err := s.installationCandidateRevision(call, id)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	if _, err := s.approveServiceRevisionForOwner(ctx, host, call, revisionID, revisionOwnerInstallation); err != nil {
		return plugininstallation.Candidate{}, err
	}
	s.recordInstallationMigrationResult(call, revisionID, "Confirmed", "")
	return s.GetPluginInstallationCandidate(call, id)
}

func (s *Service) ApproveSelfServicePluginInstallationCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, target plugininstallation.Target, evidence map[string]json.RawMessage) (plugininstallation.Candidate, error) {
	if _, err := s.GetSelfServicePluginInstallationCandidate(call, id, target); err != nil {
		return plugininstallation.Candidate{}, err
	}
	return s.ApprovePluginInstallationCandidateWithEvidence(ctx, host, call, id, evidence)
}

func (s *Service) ActivatePluginInstallationCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string) (plugininstallation.Candidate, error) {
	candidate, err := s.GetPluginInstallationCandidate(call, id)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	if candidate.Status != plugininstallation.CandidateApproved && candidate.Status != plugininstallation.CandidateReady {
		return plugininstallation.Candidate{}, errServiceState
	}
	prepared, precommitted, err := prepareInstallationPortals(ctx, host, call, candidate)
	if err != nil {
		rollbackErr := rollbackInstallationPortals(ctx, host, call, candidate.ID, precommitted)
		if candidate.Status == plugininstallation.CandidateReady {
			_, backendErr := s.RollbackServiceRevision(ctx, host, call, candidate.PreviousServiceRevisionID)
			rollbackErr = errors.Join(rollbackErr, backendErr)
		}
		return plugininstallation.Candidate{}, errors.Join(err, rollbackErr)
	}
	backendPublished := candidate.Status == plugininstallation.CandidateReady
	if !backendPublished {
		if _, err := s.publishServiceRevision(ctx, host, call, candidate.ServiceRevisionID, revisionOwnerInstallation); err != nil {
			abortInstallationPortals(ctx, host, call, candidate.ID, prepared)
			return plugininstallation.Candidate{}, err
		}
		backendPublished = true
	}
	waitCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, s.releaseTimeout)
		defer cancel()
	}
	if err := s.waitForServiceReadiness(waitCtx, host, call, candidate.Preview.Target.Deployment, candidate.ServiceRevisionID); err != nil {
		abortInstallationPortals(ctx, host, call, candidate.ID, prepared)
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.releaseTimeout)
		_, rollbackErr := s.RollbackServiceRevision(rollbackCtx, host, call, candidate.PreviousServiceRevisionID)
		cancel()
		s.recordInstallationMigrationResult(call, candidate.ServiceRevisionID, "Failed", err.Error())
		return plugininstallation.Candidate{}, errors.Join(err, rollbackErr)
	}
	s.recordInstallationMigrationResult(call, candidate.ServiceRevisionID, "Ready", "")
	committed, err := commitInstallationPortals(ctx, host, call, candidate.ID, prepared)
	if err != nil {
		rollbackErr := rollbackInstallationPortals(ctx, host, call, candidate.ID, committed)
		if backendPublished {
			_, rollbackBackendErr := s.RollbackServiceRevision(ctx, host, call, candidate.PreviousServiceRevisionID)
			rollbackErr = errors.Join(rollbackErr, rollbackBackendErr)
		}
		abortInstallationPortals(ctx, host, call, candidate.ID, prepared[len(committed):])
		return plugininstallation.Candidate{}, errors.Join(err, rollbackErr)
	}
	return s.GetPluginInstallationCandidate(call, id)
}

func (s *Service) recordInstallationMigrationResult(call *contractv1.CallContext, revisionID uint64, phase, message string) {
	tenant, err := callTenant(call)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	markInstallationMigration(state, revisionID, phase, message, s.now().Format(time.RFC3339Nano))
	_ = s.saveLocked()
}

func (s *Service) ActivateSelfServicePluginInstallationCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, target plugininstallation.Target) (plugininstallation.Candidate, error) {
	if _, err := s.GetSelfServicePluginInstallationCandidate(call, id, target); err != nil {
		return plugininstallation.Candidate{}, err
	}
	return s.ActivatePluginInstallationCandidate(ctx, host, call, id)
}

func (s *Service) CancelPluginInstallationCandidate(call *contractv1.CallContext, id string) (plugininstallation.Candidate, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	cancelledBy, err := actor(call)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	record, ok := state.InstallationCandidates[id]
	if !ok {
		return plugininstallation.Candidate{}, errNotFound
	}
	if record.CancelledAt != "" {
		return projectInstallationCandidate(state, record)
	}
	index, err := serviceRevisionIndex(state, record.ServiceRevisionID)
	if err != nil || state.Revisions[index].Status != platformadminapi.ServiceDraft {
		return plugininstallation.Candidate{}, errServiceState
	}
	oldRecord := record
	oldRevisions := append([]platformadminapi.ServiceRevision(nil), state.Revisions...)
	oldAudit := append([]platformadminapi.ServiceAuditEvent(nil), state.ServiceAudit...)
	now := s.now().Format(time.RFC3339Nano)
	record.CancelledBy, record.CancelledAt, record.UpdatedAt = cancelledBy, now, now
	state.InstallationCandidates[id] = record
	state.Revisions = append(state.Revisions[:index], state.Revisions[index+1:]...)
	filtered := state.ServiceAudit[:0]
	for _, event := range state.ServiceAudit {
		if event.RevisionID != record.ServiceRevisionID {
			filtered = append(filtered, event)
		}
	}
	state.ServiceAudit = filtered
	if err := s.saveLocked(); err != nil {
		state.InstallationCandidates[id] = oldRecord
		state.Revisions, state.ServiceAudit = oldRevisions, oldAudit
		return plugininstallation.Candidate{}, err
	}
	return projectInstallationCandidate(state, record)
}

func (s *Service) CancelSelfServicePluginInstallationCandidate(call *contractv1.CallContext, id string, target plugininstallation.Target) (plugininstallation.Candidate, error) {
	if _, err := s.GetSelfServicePluginInstallationCandidate(call, id, target); err != nil {
		return plugininstallation.Candidate{}, err
	}
	return s.CancelPluginInstallationCandidate(call, id)
}

func (s *Service) RollbackPluginInstallationCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string) (plugininstallation.Candidate, error) {
	candidate, err := s.GetPluginInstallationCandidate(call, id)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	if candidate.Status != plugininstallation.CandidateReady {
		return plugininstallation.Candidate{}, errServiceState
	}
	if _, err := s.RollbackServiceRevision(ctx, host, call, candidate.PreviousServiceRevisionID); err != nil {
		return plugininstallation.Candidate{}, err
	}
	return s.GetPluginInstallationCandidate(call, id)
}

func (s *Service) RollbackSelfServicePluginInstallationCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id string, target plugininstallation.Target) (plugininstallation.Candidate, error) {
	if _, err := s.GetSelfServicePluginInstallationCandidate(call, id, target); err != nil {
		return plugininstallation.Candidate{}, err
	}
	return s.RollbackPluginInstallationCandidate(ctx, host, call, id)
}

func validateInstallationTarget(target plugininstallation.Target) error {
	if target.Kernel != "backend" || target.Deployment == "" || target.UnitID == "" {
		return plugininstallation.ErrTargetScopeMismatch
	}
	return nil
}

func (s *Service) installationCandidateRevision(call *contractv1.CallContext, id string) (uint64, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.tenantLocked(tenant).InstallationCandidates[id]
	if !ok {
		return 0, errNotFound
	}
	if record.CancelledAt != "" {
		return 0, errServiceState
	}
	return record.ServiceRevisionID, nil
}

func installationCandidateInProgress(state *tenantState, deployment string) bool {
	for _, record := range state.InstallationCandidates {
		if record.Request.Target.Deployment != deployment || record.CancelledAt != "" {
			continue
		}
		candidate, err := projectInstallationCandidate(state, record)
		if err != nil {
			return true
		}
		switch candidate.Status {
		case plugininstallation.CandidatePlanned, plugininstallation.CandidatePendingApproval,
			plugininstallation.CandidateApproved, plugininstallation.CandidateActivating, plugininstallation.CandidateStale:
			return true
		}
	}
	return false
}

func randomInstallationCandidateID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "installation-" + hex.EncodeToString(raw[:]), nil
}
