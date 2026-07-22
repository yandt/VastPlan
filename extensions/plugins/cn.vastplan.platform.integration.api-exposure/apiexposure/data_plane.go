package apiexposure

import (
	"context"
	"errors"
	"fmt"
	"slices"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
)

func (s *Service) CreateDataPlaneDraft(_ context.Context, principal Principal, request CreateDataPlaneDraftRequest) (DataPlaneRevision, error) {
	if err := require(principal, "platform.api-exposure.edit"); err != nil {
		return DataPlaneRevision{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	resolved, err := s.resolveDataPlaneService(request.Input.Service)
	if err != nil {
		return DataPlaneRevision{}, err
	}
	if !subset(request.Input.AllowedModes, resolved.Service.SupportedModes) || request.Input.MaxObjectBytes > resolved.Service.MaxObjectBytes {
		return DataPlaneRevision{}, errors.New("Data Plane Exposure 超出服务签名能力")
	}
	exposure, err := s.newDataPlaneExposureLocked(principal.TenantID, request.BaseExposureID, request.Input)
	if err != nil {
		return DataPlaneRevision{}, err
	}
	s.state.NextRevision++
	now := s.now().UTC()
	revision := DataPlaneRevision{ID: s.state.NextRevision, Status: StatusDraft, Exposure: exposure, CreatedAt: now, UpdatedAt: now}
	s.state.DataPlaneRevisions = append(s.state.DataPlaneRevisions, revision)
	s.auditLocked(principal, exposure.ID, revision.ID, "data-plane.draft.created")
	return revision, s.saveLocked()
}

func (s *Service) TransitionDataPlane(_ context.Context, principal Principal, revisionID uint64, action string) (DataPlaneRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.dataPlaneRevisionIndexLocked(principal.TenantID, revisionID)
	if err != nil {
		return DataPlaneRevision{}, err
	}
	revision := &s.state.DataPlaneRevisions[index]
	switch action {
	case "submit":
		if err := require(principal, "platform.api-exposure.edit"); err != nil {
			return DataPlaneRevision{}, err
		}
		if revision.Status != StatusDraft {
			return DataPlaneRevision{}, ErrInvalidState
		}
		revision.Status, revision.SubmittedBy = StatusPendingApproval, principal.ID
	case "approve":
		if err := require(principal, "platform.api-exposure.approve"); err != nil {
			return DataPlaneRevision{}, err
		}
		if revision.Status != StatusPendingApproval {
			return DataPlaneRevision{}, ErrInvalidState
		}
		if revision.SubmittedBy == principal.ID {
			return DataPlaneRevision{}, ErrSelfApproval
		}
		revision.Status, revision.ApprovedBy = StatusApproved, principal.ID
	case "publish":
		if err := require(principal, "platform.api-exposure.publish"); err != nil {
			return DataPlaneRevision{}, err
		}
		if revision.Status != StatusApproved {
			return DataPlaneRevision{}, ErrInvalidState
		}
		if _, err := s.resolveDataPlaneService(revision.Exposure.Service); err != nil {
			return DataPlaneRevision{}, err
		}
		for other := range s.state.DataPlaneRevisions {
			if other != index && s.state.DataPlaneRevisions[other].Status == StatusPublished && s.state.DataPlaneRevisions[other].Exposure.ID == revision.Exposure.ID {
				s.state.DataPlaneRevisions[other].Status = StatusSuperseded
			}
		}
		revision.Status, revision.PublishedBy = StatusPublished, principal.ID
		s.state.CatalogGeneration++
	default:
		return DataPlaneRevision{}, fmt.Errorf("未知 Data Plane 生命周期操作 %q", action)
	}
	revision.UpdatedAt = s.now().UTC()
	s.auditLocked(principal, revision.Exposure.ID, revision.ID, "data-plane."+action)
	if action == "publish" {
		return *revision, s.saveAndPublishLocked()
	}
	return *revision, s.saveLocked()
}

func (s *Service) RetireDataPlane(_ context.Context, principal Principal, exposureID string) error {
	if err := require(principal, "platform.api-exposure.publish"); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.state.DataPlaneRevisions {
		revision := &s.state.DataPlaneRevisions[index]
		if revision.Exposure.ID != exposureID || revision.Exposure.TenantID != principal.TenantID || revision.Status != StatusPublished {
			continue
		}
		revision.Status, revision.UpdatedAt = StatusRetired, s.now().UTC()
		s.state.Tombstones[revision.Exposure.RouteKey] = s.now().UTC()
		s.state.CatalogGeneration++
		for leaseID, lease := range s.leases {
			if lease.DataPlaneExposureID == exposureID {
				delete(s.leases, leaseID)
				delete(s.leaseOwners, leaseID)
			}
		}
		for ticket, record := range s.tickets {
			if record.Claims.DataPlaneExposureID == exposureID {
				delete(s.tickets, ticket)
			}
		}
		s.auditLocked(principal, exposureID, revision.ID, "data-plane.retired")
		return s.saveAndPublishLocked()
	}
	return ErrNotFound
}

func (s *Service) ListDataPlanes(_ context.Context, principal Principal) ([]DataPlaneRevision, error) {
	if err := require(principal, "platform.api-exposure.read"); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []DataPlaneRevision{}
	for _, revision := range s.state.DataPlaneRevisions {
		if revision.Exposure.TenantID == principal.TenantID {
			result = append(result, revision)
		}
	}
	return result, nil
}

func (s *Service) newDataPlaneExposureLocked(tenantID, baseID string, input DataPlaneInput) (apiv1.DataPlaneExposure, error) {
	id, key, revision := "", "", uint64(1)
	if baseID != "" {
		for _, existing := range s.state.DataPlaneRevisions {
			if existing.Exposure.ID == baseID && existing.Exposure.TenantID == tenantID && existing.Status == StatusPublished {
				id, key, revision = baseID, existing.Exposure.RouteKey, existing.Exposure.Revision+1
				break
			}
		}
		if id == "" {
			return apiv1.DataPlaneExposure{}, ErrNotFound
		}
		for _, existing := range s.state.DataPlaneRevisions {
			if existing.Exposure.ID == baseID && slices.Contains([]Status{StatusDraft, StatusPendingApproval, StatusApproved}, existing.Status) {
				return apiv1.DataPlaneExposure{}, errors.New("Data Plane Exposure 已存在未完成 revision")
			}
		}
	} else {
		var err error
		id, err = apiv1.NewDataPlaneExposureID()
		if err != nil {
			return apiv1.DataPlaneExposure{}, err
		}
		key, err = s.uniqueRouteKeyLocked()
		if err != nil {
			return apiv1.DataPlaneExposure{}, err
		}
	}
	exposure := apiv1.DataPlaneExposure{
		SchemaVersion: apiv1.SchemaVersion, ID: id, Revision: revision, RouteKey: key, TenantID: tenantID,
		Hosts: append([]string(nil), input.Hosts...), Service: input.Service, DataPlaneServiceID: input.Service.ContributionID,
		AllowedModes: append([]string(nil), input.AllowedModes...), AllowedEndpointOrigins: append([]string(nil), input.AllowedEndpointOrigins...), TLSIdentityPrefix: input.TLSIdentityPrefix, Authentication: input.Authentication,
		RequiredPermissions: append([]string(nil), input.RequiredPermissions...), MaxObjectBytes: input.MaxObjectBytes,
	}
	return exposure, apiv1.ValidateDataPlaneExposure(exposure)
}

func (s *Service) dataPlaneRevisionIndexLocked(tenantID string, id uint64) (int, error) {
	for index := range s.state.DataPlaneRevisions {
		if s.state.DataPlaneRevisions[index].ID == id && s.state.DataPlaneRevisions[index].Exposure.TenantID == tenantID {
			return index, nil
		}
	}
	return 0, ErrNotFound
}

func subset(values, allowed []string) bool {
	return len(values) > 0 && !slices.ContainsFunc(values, func(value string) bool { return !slices.Contains(allowed, value) })
}
