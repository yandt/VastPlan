package deploymentmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) ListDeploymentTargets(ctx context.Context, host sdk.Host, call *contractv1.CallContext) ([]platformadminapi.DeploymentTarget, error) {
	var response struct {
		Items []deploymentpublication.Target `json:"items"`
	}
	if err := callKernelDeployment(ctx, host, call, deploymentpublication.KernelTargetsService, struct{}{}, &response); err != nil {
		return nil, err
	}
	out := make([]platformadminapi.DeploymentTarget, len(response.Items))
	for i, target := range response.Items {
		out[i] = platformadminapi.DeploymentTarget{DeploymentName: target.DeploymentName, PlatformProfile: target.PlatformProfile}
	}
	return out, nil
}

func (s *Service) ListServiceRevisions(call *contractv1.CallContext) ([]platformadminapi.ServiceRevision, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := cloneServiceRevisions(s.tenantLocked(tenant).Revisions)
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}

// ReconcileServiceReferences drains the durable reference-protection outbox.
// It is safe to call on every management read: only revisions explicitly left
// pending by publish or restart perform repository I/O.
func (s *Service) ReconcileServiceReferences(ctx context.Context, host sdk.Host, call *contractv1.CallContext) error {
	tenant, err := callTenant(call)
	if err != nil {
		return err
	}
	type pendingReference struct {
		revision           platformadminapi.ServiceRevision
		rollbackReferences []pluginv1.ArtifactReference
	}
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	pending := make([]pendingReference, 0)
	for _, revision := range state.Revisions {
		if revision.Status != platformadminapi.ServicePublished || !revision.Active || !revision.ReferencePending {
			continue
		}
		var rollbackReferences []pluginv1.ArtifactReference
		var rollbackID uint64
		for _, candidate := range state.Revisions {
			if candidate.Deployment == revision.Deployment && candidate.Status == platformadminapi.ServicePublished && candidate.ID < revision.ID && candidate.ID > rollbackID {
				rollbackReferences, rollbackID = append([]pluginv1.ArtifactReference(nil), candidate.ArtifactReferences...), candidate.ID
			}
		}
		pending = append(pending, pendingReference{revision: cloneServiceRevision(revision), rollbackReferences: rollbackReferences})
	}
	s.mu.Unlock()

	for _, item := range pending {
		if err := publishDeploymentReferences(ctx, host, call, item.revision.Deployment, item.revision.ID, item.revision.ArtifactReferences, item.rollbackReferences); err != nil {
			return err
		}
		s.mu.Lock()
		state = s.tenantLocked(tenant)
		index, lookupErr := serviceRevisionIndex(state, item.revision.ID)
		if lookupErr == nil && state.Revisions[index].Active && state.Revisions[index].ReferencePending {
			state.Revisions[index].ReferencePending = false
			state.Revisions[index].UpdatedAt = s.now().Format(time.RFC3339Nano)
			s.auditServiceLocked(state, state.Revisions[index], "service.references.synced", "repository")
			if saveErr := s.saveLocked(); saveErr != nil {
				s.mu.Unlock()
				return saveErr
			}
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *Service) CreateServiceDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, input backendcompositionv1.ApplicationComposition) (platformadminapi.ServiceRevision, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	id := state.NextRevision + 1
	composition, err := normalizeServiceComposition(input, tenant, id)
	if err != nil {
		return platformadminapi.ServiceRevision{}, errInvalid
	}
	preview, err := previewService(ctx, host, call, composition, id)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	now := s.now().Format(time.RFC3339Nano)
	revision := platformadminapi.ServiceRevision{ID: id, Deployment: composition.Metadata.Name, Status: platformadminapi.ServiceDraft, Composition: composition, Preview: preview.Deployment, PreviewDigest: preview.Digest, ArtifactReferences: preview.ArtifactReferences, ConfigurationCatalog: preview.ConfigurationCatalog, CreatedAt: now, UpdatedAt: now}
	state.NextRevision = id
	state.Revisions = append(state.Revisions, revision)
	s.auditServiceLocked(state, revision, "service.draft.created", actorOrUnknown(call))
	if err := s.saveLocked(); err != nil {
		state.Revisions = state.Revisions[:len(state.Revisions)-1]
		state.NextRevision--
		return platformadminapi.ServiceRevision{}, err
	}
	return cloneServiceRevision(revision), nil
}

func (s *Service) UpdateServiceDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id uint64, input backendcompositionv1.ApplicationComposition) (platformadminapi.ServiceRevision, error) {
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
	if state.Revisions[index].Status != platformadminapi.ServiceDraft {
		return platformadminapi.ServiceRevision{}, errServiceState
	}
	composition, err := normalizeServiceComposition(input, tenant, id)
	if err != nil || composition.Metadata.Name != state.Revisions[index].Deployment {
		return platformadminapi.ServiceRevision{}, errInvalid
	}
	preview, err := previewService(ctx, host, call, composition, id)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	old := state.Revisions[index]
	revision := old
	revision.Composition, revision.Preview, revision.PreviewDigest, revision.ArtifactReferences, revision.ConfigurationCatalog = composition, preview.Deployment, preview.Digest, preview.ArtifactReferences, preview.ConfigurationCatalog
	revision.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Revisions[index] = revision
	s.auditServiceLocked(state, revision, "service.draft.updated", actorOrUnknown(call))
	if err := s.saveLocked(); err != nil {
		state.Revisions[index] = old
		return platformadminapi.ServiceRevision{}, err
	}
	return cloneServiceRevision(revision), nil
}

func (s *Service) SubmitServiceDraft(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id uint64) (platformadminapi.ServiceRevision, error) {
	return s.transitionService(ctx, host, call, id, platformadminapi.ServiceDraft, platformadminapi.ServicePendingApproval, "service.draft.submitted", true)
}

func (s *Service) ApproveServiceRevision(call *contractv1.CallContext, id uint64) (platformadminapi.ServiceRevision, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	approver, err := actor(call)
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
	if revision.Status != platformadminapi.ServicePendingApproval {
		return platformadminapi.ServiceRevision{}, errServiceState
	}
	if revision.SubmittedBy == approver {
		return platformadminapi.ServiceRevision{}, errSeparation
	}
	old := revision
	revision.Status, revision.ApprovedBy, revision.UpdatedAt = platformadminapi.ServiceApproved, approver, s.now().Format(time.RFC3339Nano)
	state.Revisions[index] = revision
	s.auditServiceLocked(state, revision, "service.revision.approved", approver)
	if err := s.saveLocked(); err != nil {
		state.Revisions[index] = old
		return platformadminapi.ServiceRevision{}, err
	}
	return cloneServiceRevision(revision), nil
}

func (s *Service) PublishServiceRevision(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id uint64) (platformadminapi.ServiceRevision, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	publisher, err := actor(call)
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
	if revision.Status != platformadminapi.ServiceApproved && revision.Status != platformadminapi.ServicePublishing {
		return platformadminapi.ServiceRevision{}, errServiceState
	}
	if revision.Status == platformadminapi.ServiceApproved {
		revision.Status, revision.UpdatedAt = platformadminapi.ServicePublishing, s.now().Format(time.RFC3339Nano)
		state.Revisions[index] = revision
		if err := s.saveLocked(); err != nil {
			return platformadminapi.ServiceRevision{}, err
		}
	}
	var previousReferences []pluginv1.ArtifactReference
	for i := range state.Revisions {
		if state.Revisions[i].Deployment == revision.Deployment && state.Revisions[i].Active {
			previousReferences = append([]pluginv1.ArtifactReference(nil), state.Revisions[i].ArtifactReferences...)
			break
		}
	}
	if err := protectDeploymentTransition(ctx, host, call, revision.Deployment, revision.ID*2-1, previousReferences, revision.ArtifactReferences); err != nil {
		revision.Status, revision.UpdatedAt = platformadminapi.ServiceApproved, s.now().Format(time.RFC3339Nano)
		state.Revisions[index] = revision
		_ = s.saveLocked()
		return platformadminapi.ServiceRevision{}, errServicePublish
	}
	result, err := publishService(ctx, host, call, revision.Composition, revision.ID, revision.PreviewDigest)
	if err != nil {
		publishing := revision
		revision.Status = platformadminapi.ServiceApproved
		revision.UpdatedAt = s.now().Format(time.RFC3339Nano)
		state.Revisions[index] = revision
		if saveErr := s.saveLocked(); saveErr != nil {
			state.Revisions[index] = publishing
		}
		return platformadminapi.ServiceRevision{}, errServicePublish
	}
	oldRevisions := cloneServiceRevisions(state.Revisions)
	oldAuditLength, oldNextAudit := len(state.ServiceAudit), state.NextAudit
	revision.Status, revision.Active, revision.PublishedBy = platformadminapi.ServicePublished, true, publisher
	revision.ReferencePending = true
	revision.Preview, revision.PreviewDigest, revision.KVRevision, revision.ArtifactReferences, revision.ConfigurationCatalog = result.Deployment, result.Digest, result.KVRevision, result.ArtifactReferences, result.ConfigurationCatalog
	revision.UpdatedAt = s.now().Format(time.RFC3339Nano)
	for i := range state.Revisions {
		if i != index && state.Revisions[i].Deployment == revision.Deployment {
			state.Revisions[i].Active = false
		}
	}
	state.Revisions[index] = revision
	s.auditServiceLocked(state, revision, "service.revision.published", publisher)
	if err := s.saveLocked(); err != nil {
		state.Revisions = oldRevisions
		state.ServiceAudit = state.ServiceAudit[:oldAuditLength]
		state.NextAudit = oldNextAudit
		return platformadminapi.ServiceRevision{}, err
	}
	if referenceErr := publishDeploymentReferences(ctx, host, call, revision.Deployment, revision.ID, revision.ArtifactReferences, previousReferences); referenceErr != nil {
		s.auditServiceLocked(state, revision, "service.references.pending", "repository")
		_ = s.saveLocked()
	} else {
		revision.ReferencePending = false
		state.Revisions[index] = revision
		s.auditServiceLocked(state, revision, "service.references.synced", "repository")
		if err := s.saveLocked(); err != nil {
			// The durable state written before repository I/O still says pending.
			// Keep memory aligned with it so a later read retries idempotently.
			revision.ReferencePending = true
			state.Revisions[index] = revision
		}
	}
	return cloneServiceRevision(revision), nil
}

func (s *Service) RollbackServiceRevision(ctx context.Context, host sdk.Host, call *contractv1.CallContext, sourceID uint64) (platformadminapi.ServiceRevision, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	index, err := serviceRevisionIndex(state, sourceID)
	if err != nil {
		s.mu.Unlock()
		return platformadminapi.ServiceRevision{}, err
	}
	source := state.Revisions[index]
	if source.Status != platformadminapi.ServicePublished || source.Active {
		s.mu.Unlock()
		return platformadminapi.ServiceRevision{}, errServiceState
	}
	input := source.Composition
	s.mu.Unlock()
	draft, err := s.CreateServiceDraft(ctx, host, call, input)
	if err != nil {
		return platformadminapi.ServiceRevision{}, err
	}
	// Rollback is an explicit publish operation. It creates a new monotonic
	// revision from previously published content instead of moving KV backward.
	s.mu.Lock()
	state = s.tenantLocked(tenant)
	newIndex, err := serviceRevisionIndex(state, draft.ID)
	if err != nil {
		s.mu.Unlock()
		return platformadminapi.ServiceRevision{}, err
	}
	revision := state.Revisions[newIndex]
	revision.Status = platformadminapi.ServicePublishing
	state.Revisions[newIndex] = revision
	s.auditServiceLocked(state, revision, "service.revision.rollback_started", actorOrUnknown(call))
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		return platformadminapi.ServiceRevision{}, err
	}
	s.mu.Unlock()
	return s.PublishServiceRevision(ctx, host, call, draft.ID)
}

func (s *Service) ListServiceRevisionAudit(call *contractv1.CallContext, revisionID uint64) ([]platformadminapi.ServiceAuditEvent, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	if _, err := serviceRevisionIndex(state, revisionID); err != nil {
		return nil, err
	}
	out := make([]platformadminapi.ServiceAuditEvent, 0)
	for _, event := range state.ServiceAudit {
		if event.RevisionID == revisionID {
			out = append(out, event)
		}
	}
	return out, nil
}

func (s *Service) transitionService(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id uint64, from, to platformadminapi.ServiceRevisionStatus, action string, refresh bool) (platformadminapi.ServiceRevision, error) {
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
	if revision.Status != from {
		return platformadminapi.ServiceRevision{}, errServiceState
	}
	if refresh {
		preview, err := previewService(ctx, host, call, revision.Composition, revision.ID)
		if err != nil {
			return platformadminapi.ServiceRevision{}, err
		}
		revision.Preview, revision.PreviewDigest, revision.ArtifactReferences, revision.ConfigurationCatalog = preview.Deployment, preview.Digest, preview.ArtifactReferences, preview.ConfigurationCatalog
	}
	old := state.Revisions[index]
	revision.Status, revision.UpdatedAt = to, s.now().Format(time.RFC3339Nano)
	if to == platformadminapi.ServicePendingApproval {
		revision.SubmittedBy = principal
	}
	state.Revisions[index] = revision
	s.auditServiceLocked(state, revision, action, principal)
	if err := s.saveLocked(); err != nil {
		state.Revisions[index] = old
		return platformadminapi.ServiceRevision{}, err
	}
	return cloneServiceRevision(revision), nil
}

func normalizeServiceComposition(input backendcompositionv1.ApplicationComposition, tenantID string, revision uint64) (backendcompositionv1.ApplicationComposition, error) {
	name := strings.TrimSpace(input.Metadata.Name)
	if name == "" {
		return backendcompositionv1.ApplicationComposition{}, errInvalid
	}
	input.Document = compositioncommonv1.Document{Version: 1, Revision: revision, ID: name}
	input.Target = compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend}
	input.Metadata.Name, input.Metadata.Tenant = name, tenantID
	return backendcompositionv1.ValidateApplicationComposition(input)
}

func previewService(ctx context.Context, host sdk.Host, call *contractv1.CallContext, composition backendcompositionv1.ApplicationComposition, revision uint64) (deploymentpublication.Result, error) {
	var result deploymentpublication.Result
	err := callKernelDeployment(ctx, host, call, deploymentpublication.KernelPreviewService, deploymentpublication.PreviewRequest{Composition: composition, DeploymentRevision: revision}, &result)
	return result, err
}

func publishService(ctx context.Context, host sdk.Host, call *contractv1.CallContext, composition backendcompositionv1.ApplicationComposition, revision uint64, digest string) (deploymentpublication.Result, error) {
	var result deploymentpublication.Result
	err := callKernelDeployment(ctx, host, call, deploymentpublication.KernelPublishService, deploymentpublication.PublishRequest{Composition: composition, DeploymentRevision: revision, ExpectedDigest: digest}, &result)
	return result, err
}

func callKernelDeployment(ctx context.Context, host sdk.Host, call *contractv1.CallContext, capability string, request, response any) error {
	if host == nil {
		return errServicePublish
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	operation := "execute"
	result, payload, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: capability, Operation: &operation}, call, raw)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return fmt.Errorf("%w: %s", errServicePublish, capability)
	}
	if response == nil {
		return nil
	}
	if err := json.Unmarshal(payload, response); err != nil {
		return fmt.Errorf("%w: %s response", errServicePublish, capability)
	}
	return nil
}

func serviceRevisionIndex(state *tenantState, id uint64) (int, error) {
	for i := range state.Revisions {
		if state.Revisions[i].ID == id {
			return i, nil
		}
	}
	return 0, errNotFound
}

func (s *Service) auditServiceLocked(state *tenantState, revision platformadminapi.ServiceRevision, action, principal string) {
	state.NextAudit++
	state.ServiceAudit = append(state.ServiceAudit, platformadminapi.ServiceAuditEvent{ID: state.NextAudit, RevisionID: revision.ID, Deployment: revision.Deployment, Action: action, ActorID: principal, At: s.now().Format(time.RFC3339Nano)})
}

func actorOrUnknown(call *contractv1.CallContext) string {
	value, err := actor(call)
	if err != nil {
		return "unknown"
	}
	return value
}

func cloneServiceRevision(in platformadminapi.ServiceRevision) platformadminapi.ServiceRevision {
	raw, _ := json.Marshal(in)
	var out platformadminapi.ServiceRevision
	_ = json.Unmarshal(raw, &out)
	return out
}

func cloneServiceRevisions(in []platformadminapi.ServiceRevision) []platformadminapi.ServiceRevision {
	out := make([]platformadminapi.ServiceRevision, len(in))
	for i := range in {
		out[i] = cloneServiceRevision(in[i])
	}
	return out
}
