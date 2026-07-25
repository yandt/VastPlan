package deploymentmanager

import (
	"context"
	"time"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) CreateIntentDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, input backendcompositionv1.ApplicationIntent) (platformadminapi.ServiceRevision, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	id := state.NextRevision + 1
	intent, err := normalizeApplicationIntent(input, tenant, id)
	if err != nil {
		return platformadminapi.ServiceRevision{}, errInvalid
	}
	plan, err := buildIntentPlan(ctx, host, call, intent, nil, id)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	now := s.now().Format(time.RFC3339Nano)
	revision := platformadminapi.ServiceRevision{ID: id, Deployment: intent.Metadata.Name, Status: platformadminapi.ServiceDraft, Intent: &intent, CreatedAt: now, UpdatedAt: now}
	applyIntentPlan(&revision, plan)
	state.NextRevision = id
	state.Revisions = append(state.Revisions, revision)
	oldAuditLength, oldNextAudit := len(state.ServiceAudit), state.NextAudit
	s.auditServiceLocked(state, revision, "service.intent.created", actorOrUnknown(call))
	if err := s.saveLocked(); err != nil {
		state.Revisions = state.Revisions[:len(state.Revisions)-1]
		state.NextRevision--
		state.ServiceAudit = state.ServiceAudit[:oldAuditLength]
		state.NextAudit = oldNextAudit
		return platformadminapi.ServiceRevision{}, err
	}
	return cloneServiceRevision(revision), nil
}

func (s *Service) UpdateIntentDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id uint64, input backendcompositionv1.ApplicationIntent) (platformadminapi.ServiceRevision, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	index, err := serviceRevisionIndex(state, id)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	old := state.Revisions[index]
	if old.Status != platformadminapi.ServiceDraft || old.Intent == nil {
		return platformadminapi.ServiceRevision{}, errServiceState
	}
	intent, err := normalizeApplicationIntent(input, tenant, id)
	if err != nil || intent.Metadata.Name != old.Deployment {
		return platformadminapi.ServiceRevision{}, errInvalid
	}
	plan, err := buildIntentPlan(ctx, host, call, intent, old.ConfigurationSnapshot, id)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	revision := old
	revision.Intent = &intent
	clearIntentApproval(&revision)
	applyIntentPlan(&revision, plan)
	revision.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Revisions[index] = revision
	oldAuditLength, oldNextAudit := len(state.ServiceAudit), state.NextAudit
	s.auditServiceLocked(state, revision, "service.intent.updated", actorOrUnknown(call))
	if err := s.saveLocked(); err != nil {
		state.Revisions[index] = old
		state.ServiceAudit = state.ServiceAudit[:oldAuditLength]
		state.NextAudit = oldNextAudit
		return platformadminapi.ServiceRevision{}, err
	}
	return cloneServiceRevision(revision), nil
}

func (s *Service) RefreshIntentPlan(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id uint64) (platformadminapi.ServiceRevision, error) {
	return s.replaceIntentPlan(ctx, host, call, id, nil, false, "service.intent.plan_refreshed")
}

func (s *Service) BindIntentConfiguration(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id uint64, snapshot backendcompositionv1.PlanningConfigurationSnapshot) (platformadminapi.ServiceRevision, error) {
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != pluginconfiguration.PluginSettingsID {
		return platformadminapi.ServiceRevision{}, errInvalid
	}
	return s.replaceIntentPlan(ctx, host, call, id, &snapshot, true, "service.intent.configuration_bound")
}

func (s *Service) replaceIntentPlan(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id uint64, snapshot *backendcompositionv1.PlanningConfigurationSnapshot, replaceSnapshot bool, action string) (platformadminapi.ServiceRevision, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	index, err := serviceRevisionIndex(state, id)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	old := state.Revisions[index]
	if old.Status != platformadminapi.ServiceDraft || old.Intent == nil {
		return platformadminapi.ServiceRevision{}, errServiceState
	}
	trusted := old.ConfigurationSnapshot
	if replaceSnapshot {
		trusted = snapshot
	}
	plan, err := buildIntentPlan(ctx, host, call, *old.Intent, trusted, old.ID)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	revision := old
	revision.ConfigurationSnapshot = clonePlanningSnapshot(trusted)
	clearIntentApproval(&revision)
	applyIntentPlan(&revision, plan)
	revision.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Revisions[index] = revision
	oldAuditLength, oldNextAudit := len(state.ServiceAudit), state.NextAudit
	s.auditServiceLocked(state, revision, action, actorOrUnknown(call))
	if err := s.saveLocked(); err != nil {
		state.Revisions[index] = old
		state.ServiceAudit = state.ServiceAudit[:oldAuditLength]
		state.NextAudit = oldNextAudit
		return platformadminapi.ServiceRevision{}, err
	}
	return cloneServiceRevision(revision), nil
}

func (s *Service) submitIntentDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id uint64) (platformadminapi.ServiceRevision, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	principal, err := actor(call)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	index, err := serviceRevisionIndex(state, id)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	revision := state.Revisions[index]
	if revision.Status != platformadminapi.ServiceDraft || revision.Intent == nil || revision.ResolutionReport == nil {
		return platformadminapi.ServiceRevision{}, errServiceState
	}
	if revision.PlanningStale {
		return platformadminapi.ServiceRevision{}, errPlanStale
	}
	if err := s.requireFreshIntentPlanLocked(ctx, host, call, state, index); err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	revision = state.Revisions[index]
	old := revision
	revision.Status, revision.SubmittedBy = platformadminapi.ServicePendingApproval, principal
	revision.SubmittedPlanDigest = revision.ResolutionReport.PlanDigest
	revision.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Revisions[index] = revision
	oldAuditLength, oldNextAudit := len(state.ServiceAudit), state.NextAudit
	s.auditServiceLocked(state, revision, "service.intent.submitted", principal)
	if err := s.saveLocked(); err != nil {
		state.Revisions[index] = old
		state.ServiceAudit = state.ServiceAudit[:oldAuditLength]
		state.NextAudit = oldNextAudit
		return platformadminapi.ServiceRevision{}, err
	}
	return cloneServiceRevision(revision), nil
}

func (s *Service) requireFreshIntentPlanLocked(ctx context.Context, host sdk.Host, call *contractv1.CallContext, state *tenantState, index int) error {
	revision := state.Revisions[index]
	current, err := buildIntentPlan(ctx, host, call, *revision.Intent, revision.ConfigurationSnapshot, revision.ID)
	if err != nil {
		return err
	}
	if !sameIntentPlan(*revision.ResolutionReport, revision.PreviewDigest, current) {
		markIntentStale(&revision, current.report.PlanDigest, "Planner、Platform Profile、Catalog 或配置摘要已变化")
		state.Revisions[index] = revision
		s.auditServiceLocked(state, revision, "service.intent.stale", "planner")
		if saveErr := s.saveLocked(); saveErr != nil {
			return saveErr
		}
		return errPlanStale
	}
	if current.report.Status != backendcompositionv1.ResolutionResolved || current.preview == nil {
		return errPlanNotReady
	}
	return nil
}

func applyIntentPlan(revision *platformadminapi.ServiceRevision, plan intentPlan) {
	report := cloneJSON(plan.report)
	revision.ResolutionReport = &report
	revision.PlanningStale, revision.PlanningStaleReason, revision.ObservedPlanDigest = false, "", ""
	revision.Composition = backendcompositionv1.ApplicationComposition{}
	revision.PreviewDigest = ""
	if report.ApplicationComposition != nil {
		revision.Composition = cloneJSON(*report.ApplicationComposition)
	}
	if plan.preview != nil {
		revision.Preview = cloneJSON(plan.preview.Deployment)
		revision.PreviewDigest = plan.preview.Digest
		revision.ArtifactReferences = append(revision.ArtifactReferences[:0], plan.preview.ArtifactReferences...)
		revision.ConfigurationCatalog = cloneJSON(plan.preview.ConfigurationCatalog)
	} else {
		revision.Preview = deploymentv2.Deployment{}
		revision.ArtifactReferences = nil
		revision.ConfigurationCatalog = pluginconfiguration.Catalog{}
	}
}

func clearIntentApproval(revision *platformadminapi.ServiceRevision) {
	revision.Status = platformadminapi.ServiceDraft
	revision.SubmittedBy, revision.ApprovedBy = "", ""
	revision.SubmittedPlanDigest, revision.ApprovedPlanDigest = "", ""
}

func markIntentStale(revision *platformadminapi.ServiceRevision, observed, reason string) {
	clearIntentApproval(revision)
	revision.PlanningStale, revision.PlanningStaleReason, revision.ObservedPlanDigest = true, reason, observed
}

func clonePlanningSnapshot(snapshot *backendcompositionv1.PlanningConfigurationSnapshot) *backendcompositionv1.PlanningConfigurationSnapshot {
	if snapshot == nil {
		return nil
	}
	copy := cloneJSON(*snapshot)
	return &copy
}

func isIntentRevision(revision platformadminapi.ServiceRevision) bool {
	return revision.Intent != nil || revision.ResolutionReport != nil
}
