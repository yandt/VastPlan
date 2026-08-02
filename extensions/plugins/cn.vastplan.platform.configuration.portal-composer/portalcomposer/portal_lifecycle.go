package portalcomposer

import (
	"context"
	"errors"
	"fmt"
	"time"

	approvalv1 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func (s *Service) CreatePortalWorkingCopy(ctx context.Context, principal portalapi.Principal, portalID string, configuration portalapi.PortalConfiguration) (portalapi.PortalWorkingCopy, error) {
	version, err := s.CreatePortalVersion(ctx, principal, portalID, configuration)
	if err != nil {
		return portalapi.PortalWorkingCopy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.revisionIndex(principal.TenantID, version.ID)
	if err != nil {
		return portalapi.PortalWorkingCopy{}, err
	}
	return s.portalWorkingCopyLocked(principal.TenantID, s.state.Revisions[index])
}

func (s *Service) SavePortalWorkingCopy(ctx context.Context, principal portalapi.Principal, portalID string, request portalapi.SavePortalWorkingCopyRequest) (portalapi.PortalWorkingCopy, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.PortalWorkingCopy{}, err
	}
	if request.ExpectedRevision == 0 {
		return portalapi.PortalWorkingCopy{}, ErrInvalidState
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.workingCopyIndexLocked(principal.TenantID, portalID)
	if err != nil {
		return portalapi.PortalWorkingCopy{}, err
	}
	if s.state.Revisions[index].WorkingRevision != request.ExpectedRevision {
		return portalapi.PortalWorkingCopy{}, fmt.Errorf("%w: WorkingCopy revision 已从 %d 变为 %d", ErrInvalidState, request.ExpectedRevision, s.state.Revisions[index].WorkingRevision)
	}
	if err := s.updateWorkingCopyLocked(ctx, principal, index, request.Configuration, "portal.working-copy.saved"); err != nil {
		return portalapi.PortalWorkingCopy{}, err
	}
	return s.portalWorkingCopyLocked(principal.TenantID, s.state.Revisions[index])
}

func (s *Service) SubmitPortalPublication(ctx context.Context, principal portalapi.Principal, portalID string, request portalapi.SubmitPortalPublicationRequest) (portalapi.PortalPublication, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.PortalPublication{}, err
	}
	if request.ExpectedWorkingRevision == 0 {
		return portalapi.PortalPublication{}, ErrInvalidState
	}
	s.mu.Lock()
	index, err := s.workingCopyIndexLocked(principal.TenantID, portalID)
	if err != nil {
		// A successful response may be lost after the aggregate CAS. Returning
		// the already frozen candidate makes an identical submit retry safe.
		for _, revision := range s.state.Revisions {
			if revision.TenantID == principal.TenantID && revision.PortalID == portalID && revision.WorkingRevision == request.ExpectedWorkingRevision && revision.Status != portalapi.StatusDraft {
				publication, projectionErr := s.portalPublicationLocked(principal.TenantID, revision)
				s.mu.Unlock()
				return publication, projectionErr
			}
		}
		s.mu.Unlock()
		return portalapi.PortalPublication{}, err
	}
	if s.state.Revisions[index].WorkingRevision != request.ExpectedWorkingRevision {
		s.mu.Unlock()
		return portalapi.PortalPublication{}, fmt.Errorf("%w: WorkingCopy revision 已从 %d 变为 %d", ErrInvalidState, request.ExpectedWorkingRevision, s.state.Revisions[index].WorkingRevision)
	}
	control, versioned := s.state.VersionControls[portalID]
	if !versioned {
		publication, transitionErr := s.transitionPublicationLocked(ctx, principal, index, "submit", "portal.publication.", "")
		s.mu.Unlock()
		return publication, transitionErr
	}
	publication, err := s.submitVersionedPortalPublicationLocked(ctx, principal, index, control)
	s.mu.Unlock()
	return publication, err
}

func (s *Service) ApprovePortalPublication(ctx context.Context, principal portalapi.Principal, portalID string, publicationID uint64, request portalapi.PortalApprovalRequest) (portalapi.PortalPublication, error) {
	return s.transitionPortalPublication(ctx, principal, portalID, publicationID, "approve", request)
}

func (s *Service) PublishPortalPublication(ctx context.Context, principal portalapi.Principal, portalID string, publicationID uint64) (portalapi.PortalPublication, error) {
	return s.transitionPortalPublication(ctx, principal, portalID, publicationID, "publish", portalapi.PortalApprovalRequest{})
}

func (s *Service) transitionPortalPublication(ctx context.Context, principal portalapi.Principal, portalID string, publicationID uint64, action string, approval portalapi.PortalApprovalRequest) (portalapi.PortalPublication, error) {
	if err := requireTrustedPrincipal(principal); err != nil {
		return portalapi.PortalPublication{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.revisionIndex(principal.TenantID, publicationID)
	if err != nil || s.state.Revisions[index].PortalID != portalID || s.isTestVersionLocked(publicationID) {
		return portalapi.PortalPublication{}, ErrNotFound
	}
	auditReason := ""
	if action == "approve" {
		decision := s.approvalDecision(principal, s.state.Revisions[index], approval.Review)
		if decision.Status != approvalv1.DecisionAllowed {
			err := &ApprovalError{Decision: decision}
			return portalapi.PortalPublication{}, err
		}
		auditReason = decision.AuditNote
	}
	return s.transitionPublicationLocked(ctx, principal, index, action, "portal.publication.", auditReason)
}

func (s *Service) ReleasePortalPublication(ctx context.Context, principal portalapi.Principal, portalID string, request portalapi.PortalPublicationReleaseRequest) (portalapi.PortalRelease, error) {
	return s.ReleasePortalVersion(ctx, principal, portalID, portalapi.PortalReleaseRequest{
		PortalVersionID: request.PublicationID, ExpectedCurrentReleaseID: request.ExpectedCurrentReleaseID, Reason: request.Reason,
	})
}

func (s *Service) updateWorkingCopyLocked(ctx context.Context, principal portalapi.Principal, index int, configuration portalapi.PortalConfiguration, auditAction string) error {
	revision := &s.state.Revisions[index]
	if revision.Status != portalapi.StatusDraft || s.isTestVersionLocked(revision.ID) {
		return ErrInvalidState
	}
	if control, enabled := s.state.VersionControls[revision.PortalID]; enabled && control.Pending != nil {
		return fmt.Errorf("%w: Portal 版本提交正在恢复，WorkingCopy 暂不可修改", ErrInvalidState)
	}
	configuration, spec, binding, err := s.normalizePortalConfiguration(revision.PortalID, principal.TenantID, revision.Number, revision.ID, configuration)
	if err != nil {
		return err
	}
	if err := s.validateCatalog(ctx, principal.TenantID, spec); err != nil {
		return fmt.Errorf("%w: %v", ErrCatalogRejected, err)
	}
	profileIndex, bindingIndex, err := s.versionPartsLocked(principal.TenantID, *revision)
	if err != nil {
		return err
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	revision.Composition, revision.Spec, revision.UpdatedAt, revision.UpdatedBy = configuration.Application, spec, now, principal.ID
	revision.WorkingRevision++
	revision.ConfigurationDigest = ""
	s.state.Profiles[profileIndex].Profile, s.state.Profiles[profileIndex].UpdatedAt = configuration.Platform, now
	s.state.Bindings[bindingIndex].Binding, s.state.Bindings[bindingIndex].UpdatedAt = binding, now
	s.auditLocked(*revision, auditAction, principal, "", "normal")
	return s.save()
}

func (s *Service) transitionPublicationLocked(ctx context.Context, principal portalapi.Principal, index int, action, auditPrefix, auditReason string) (portalapi.PortalPublication, error) {
	revision := &s.state.Revisions[index]
	if s.isTestVersionLocked(revision.ID) {
		return portalapi.PortalPublication{}, ErrNotFound
	}
	profileIndex, bindingIndex, err := s.versionPartsLocked(principal.TenantID, *revision)
	if err != nil {
		return portalapi.PortalPublication{}, err
	}
	profile, binding := &s.state.Profiles[profileIndex], &s.state.Bindings[bindingIndex]
	if profile.Status != revision.Status || binding.Status != revision.Status {
		return portalapi.PortalPublication{}, errors.New("Portal Publication 内部状态不一致")
	}
	next, err := transitionStatus(principal, revision.Status, action)
	if err != nil {
		return portalapi.PortalPublication{}, err
	}
	if action == "submit" || action == "publish" {
		configuration := portalapi.PortalConfiguration{Platform: profile.Profile, Application: revision.Composition, Services: binding.Binding.Services}
		configuration, spec, _, normalizeErr := s.normalizePortalConfiguration(revision.PortalID, principal.TenantID, revision.Number, revision.ID, configuration)
		if normalizeErr != nil {
			return portalapi.PortalPublication{}, normalizeErr
		}
		if catalogErr := s.validateCatalog(ctx, principal.TenantID, spec); catalogErr != nil {
			return portalapi.PortalPublication{}, fmt.Errorf("%w: %v", ErrCatalogRejected, catalogErr)
		}
		digest, digestErr := portalConfigurationDigest(configuration)
		if digestErr != nil {
			return portalapi.PortalPublication{}, digestErr
		}
		if action == "submit" {
			revision.ConfigurationDigest = digest
		} else if revision.ConfigurationDigest == "" || revision.ConfigurationDigest != digest {
			return portalapi.PortalPublication{}, errors.New("Portal Publication 冻结内容摘要不一致")
		}
		revision.Spec = spec
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	revision.Status, profile.Status, binding.Status = next, next, next
	revision.UpdatedAt, profile.UpdatedAt, binding.UpdatedAt = now, now, now
	if action == "submit" {
		revision.SubmittedAt = now
	}
	applyActors(&revision.SubmittedBy, &revision.ApprovedBy, &revision.PublishedBy, principal.ID, action)
	applyActors(&profile.SubmittedBy, &profile.ApprovedBy, &profile.PublishedBy, principal.ID, action)
	applyActors(&binding.SubmittedBy, &binding.ApprovedBy, &binding.PublishedBy, principal.ID, action)
	s.auditLocked(*revision, auditPrefix+action, principal, auditReason, "normal")
	if err := s.save(); err != nil {
		return portalapi.PortalPublication{}, err
	}
	return s.portalPublicationLocked(principal.TenantID, *revision)
}
