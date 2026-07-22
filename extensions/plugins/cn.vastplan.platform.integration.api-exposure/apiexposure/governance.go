package apiexposure

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
)

var (
	ErrForbidden    = errors.New("API Exposure 操作未授权")
	ErrNotFound     = errors.New("API Exposure revision 不存在")
	ErrInvalidState = errors.New("API Exposure 状态不允许当前操作")
	ErrSelfApproval = errors.New("API Exposure 提交人与审批人必须分离")
)

func (s *Service) CreateDraft(_ context.Context, principal Principal, request CreateDraftRequest) (Revision, error) {
	if err := require(principal, "platform.api-exposure.edit"); err != nil {
		return Revision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.configured {
		return Revision{}, errors.New("API Exposure 尚未配置")
	}
	resolved, err := s.resolveContract(request.Contract)
	if err != nil {
		return Revision{}, err
	}
	exposure, err := s.newExposureLocked(principal.TenantID, request.BaseExposureID, request.Input, resolved.Reference)
	if err != nil {
		return Revision{}, err
	}
	s.state.NextRevision++
	now := s.now().UTC()
	revision := Revision{ID: s.state.NextRevision, Status: StatusDraft, Exposure: exposure, Contract: resolved.Contract, CreatedAt: now, UpdatedAt: now}
	s.state.Revisions = append(s.state.Revisions, revision)
	s.auditLocked(principal, exposure.ID, revision.ID, "http.draft.created")
	return revision, s.saveLocked()
}

func (s *Service) UpdateDraft(_ context.Context, principal Principal, request UpdateDraftRequest) (Revision, error) {
	if err := require(principal, "platform.api-exposure.edit"); err != nil {
		return Revision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.revisionIndexLocked(principal.TenantID, request.RevisionID)
	if err != nil {
		return Revision{}, err
	}
	revision := &s.state.Revisions[index]
	if revision.Status != StatusDraft || revision.Exposure.Revision != request.ExpectedRevision {
		return Revision{}, ErrInvalidState
	}
	resolved, err := s.resolveContract(request.Contract)
	if err != nil {
		return Revision{}, err
	}
	next := exposureFromInput(revision.Exposure.ID, revision.Exposure.RouteKey, revision.Exposure.Revision+1, principal.TenantID, request.Input, resolved.Reference)
	if err := apiv1.ValidateExposure(next); err != nil {
		return Revision{}, err
	}
	revision.Exposure, revision.Contract, revision.UpdatedAt = next, resolved.Contract, s.now().UTC()
	s.auditLocked(principal, next.ID, revision.ID, "http.draft.updated")
	return *revision, s.saveLocked()
}

func (s *Service) Transition(_ context.Context, principal Principal, revisionID uint64, action string) (Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.revisionIndexLocked(principal.TenantID, revisionID)
	if err != nil {
		return Revision{}, err
	}
	revision := &s.state.Revisions[index]
	switch action {
	case "submit":
		if err := require(principal, "platform.api-exposure.edit"); err != nil || revision.Status != StatusDraft {
			if err != nil {
				return Revision{}, err
			}
			return Revision{}, ErrInvalidState
		}
		revision.Status, revision.SubmittedBy = StatusPendingApproval, principal.ID
	case "approve":
		if err := require(principal, "platform.api-exposure.approve"); err != nil {
			return Revision{}, err
		}
		if revision.Status != StatusPendingApproval {
			return Revision{}, ErrInvalidState
		}
		if revision.SubmittedBy == principal.ID {
			return Revision{}, ErrSelfApproval
		}
		revision.Status, revision.ApprovedBy = StatusApproved, principal.ID
	case "publish":
		if err := require(principal, "platform.api-exposure.publish"); err != nil {
			return Revision{}, err
		}
		if revision.Status != StatusApproved {
			return Revision{}, ErrInvalidState
		}
		selector := ContractSelector{PluginID: revision.Exposure.Contract.PluginID, ArtifactSHA256: revision.Exposure.Contract.ArtifactSHA256, ContributionID: revision.Exposure.Contract.ContributionID}
		resolved, err := s.resolveContract(selector)
		if err != nil || resolved.Reference.ContractDigest != revision.Exposure.Contract.ContractDigest {
			return Revision{}, errors.New("发布时 Contract 已不在当前可信 Catalog")
		}
		for other := range s.state.Revisions {
			if other != index && s.state.Revisions[other].Status == StatusPublished && s.state.Revisions[other].Exposure.ID == revision.Exposure.ID {
				s.state.Revisions[other].Status = StatusSuperseded
			}
		}
		revision.Status, revision.PublishedBy = StatusPublished, principal.ID
		s.state.CatalogGeneration++
	default:
		return Revision{}, fmt.Errorf("未知 API Exposure 生命周期操作 %q", action)
	}
	revision.UpdatedAt = s.now().UTC()
	s.auditLocked(principal, revision.Exposure.ID, revision.ID, "http."+action)
	if action == "publish" {
		return *revision, s.saveAndPublishLocked()
	}
	return *revision, s.saveLocked()
}

func (s *Service) Retire(_ context.Context, principal Principal, exposureID string) error {
	if err := require(principal, "platform.api-exposure.publish"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.state.Revisions {
		revision := &s.state.Revisions[index]
		if revision.Exposure.ID != exposureID || revision.Exposure.TenantID != principal.TenantID || revision.Status != StatusPublished {
			continue
		}
		revision.Status, revision.UpdatedAt = StatusRetired, s.now().UTC()
		s.state.Tombstones[revision.Exposure.RouteKey] = s.now().UTC()
		s.state.CatalogGeneration++
		s.auditLocked(principal, exposureID, revision.ID, "http.retired")
		return s.saveAndPublishLocked()
	}
	return ErrNotFound
}

func (s *Service) List(_ context.Context, principal Principal) ([]Revision, error) {
	if err := require(principal, "platform.api-exposure.read"); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Revision, 0)
	for _, revision := range s.state.Revisions {
		if revision.Exposure.TenantID == principal.TenantID {
			result = append(result, revision)
		}
	}
	return result, nil
}

func (s *Service) newExposureLocked(tenantID, baseID string, input ExposureInput, reference apiv1.ContractReference) (apiv1.Exposure, error) {
	if baseID != "" {
		for _, revision := range s.state.Revisions {
			if revision.Exposure.ID == baseID && revision.Exposure.TenantID == tenantID && revision.Status == StatusPublished {
				if s.hasOpenHTTPRevisionLocked(baseID) {
					return apiv1.Exposure{}, errors.New("API Exposure 已存在未完成 revision")
				}
				exposure := exposureFromInput(baseID, revision.Exposure.RouteKey, revision.Exposure.Revision+1, tenantID, input, reference)
				return exposure, apiv1.ValidateExposure(exposure)
			}
		}
		return apiv1.Exposure{}, ErrNotFound
	}
	id, err := apiv1.NewExposureID()
	if err != nil {
		return apiv1.Exposure{}, err
	}
	key, err := s.uniqueRouteKeyLocked()
	if err != nil {
		return apiv1.Exposure{}, err
	}
	exposure := exposureFromInput(id, key, 1, tenantID, input, reference)
	return exposure, apiv1.ValidateExposure(exposure)
}

func exposureFromInput(id, key string, revision uint64, tenantID string, input ExposureInput, reference apiv1.ContractReference) apiv1.Exposure {
	return apiv1.Exposure{
		SchemaVersion: apiv1.SchemaVersion, ID: id, Revision: revision, RouteKey: key, DisplayName: input.DisplayName,
		TenantID: tenantID, PortalID: input.PortalID, Hosts: append([]string(nil), input.Hosts...), Contract: reference,
		Authentication: input.Authentication, RequiredPermissions: append([]string(nil), input.RequiredPermissions...), Limits: input.Limits, Target: input.Target,
	}
}

func (s *Service) uniqueRouteKeyLocked() (string, error) {
	for range 8 {
		key, err := apiv1.NewRouteKey()
		if err != nil {
			return "", err
		}
		if _, tombstoned := s.state.Tombstones[key]; tombstoned {
			continue
		}
		used := false
		for _, revision := range s.state.Revisions {
			if revision.Exposure.RouteKey == key {
				used = true
				break
			}
		}
		if !used {
			for _, revision := range s.state.DataPlaneRevisions {
				if revision.Exposure.RouteKey == key {
					used = true
					break
				}
			}
		}
		if !used {
			return key, nil
		}
	}
	return "", errors.New("无法分配唯一 Route Key")
}

func (s *Service) hasOpenHTTPRevisionLocked(exposureID string) bool {
	for _, revision := range s.state.Revisions {
		if revision.Exposure.ID == exposureID && slices.Contains([]Status{StatusDraft, StatusPendingApproval, StatusApproved}, revision.Status) {
			return true
		}
	}
	return false
}

func (s *Service) revisionIndexLocked(tenantID string, id uint64) (int, error) {
	for index := range s.state.Revisions {
		if s.state.Revisions[index].ID == id && s.state.Revisions[index].Exposure.TenantID == tenantID {
			return index, nil
		}
	}
	return 0, ErrNotFound
}

func (s *Service) auditLocked(principal Principal, resourceID string, revisionID uint64, action string) {
	s.state.NextAudit++
	s.state.Audit = append(s.state.Audit, AuditEvent{ID: s.state.NextAudit, TenantID: principal.TenantID, ResourceID: resourceID, RevisionID: revisionID, Action: action, Actor: principal.ID, At: s.now().UTC()})
}

func require(principal Principal, role string) error {
	if strings.TrimSpace(principal.ID) == "" || strings.TrimSpace(principal.TenantID) == "" || !slices.Contains(principal.Roles, role) {
		return ErrForbidden
	}
	return nil
}
