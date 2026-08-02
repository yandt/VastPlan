package portalcomposer

import (
	"context"
	"sort"

	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

// PortalGovernance projects the hot Portal aggregate and enriches pending
// Publications through external Approval and optional Version Control ports.
func (s *Service) PortalGovernance(ctx context.Context, principal portalapi.Principal) (portalapi.PortalGovernanceSnapshot, error) {
	if principal.ID == "" || principal.TenantID == "" {
		return portalapi.PortalGovernanceSnapshot{}, ErrForbidden
	}
	_ = s.reconcilePortalReferences(ctx, principal)
	portals, approvalPortalIDs, approvalRevisions, bindings, template, err := s.governanceSnapshotLocked(principal.TenantID)
	if err != nil {
		return portalapi.PortalGovernanceSnapshot{}, err
	}
	s.projectApprovalDecisions(ctx, principal, portals, approvalPortalIDs, approvalRevisions)
	s.projectVersionControl(ctx, portals, bindings)
	return portalapi.PortalGovernanceSnapshot{Portals: portals, CreationTemplate: template}, nil
}

func (s *Service) governanceSnapshotLocked(tenantID string) ([]portalapi.Portal, []string, []portalapi.Revision, map[string]PortalVersionControlBinding, *portalapi.PortalConfiguration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := map[string]struct{}{}
	for _, revision := range s.state.Revisions {
		if revision.TenantID == tenantID && !s.isTestVersionLocked(revision.ID) {
			ids[revision.PortalID] = struct{}{}
		}
	}
	portals := make([]portalapi.Portal, 0, len(ids))
	approvalPortalIDs := make([]string, 0)
	approvalRevisions := make([]portalapi.Revision, 0)
	for id := range ids {
		portal, err := s.portalLocked(tenantID, id)
		if err != nil {
			return nil, nil, nil, nil, nil, err
		}
		if portal.PendingPublication != nil && portal.PendingPublication.Status == portalapi.StatusPendingApproval {
			index, err := s.revisionIndex(tenantID, portal.PendingPublication.ID)
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
			approvalPortalIDs = append(approvalPortalIDs, portal.ID)
			approvalRevisions = append(approvalRevisions, s.state.Revisions[index])
		}
		portals = append(portals, portal)
	}
	sort.Slice(portals, func(i, j int) bool { return portals[i].ID < portals[j].ID })
	bindings := make(map[string]PortalVersionControlBinding, len(s.state.VersionControls))
	for portalID, control := range s.state.VersionControls {
		bindings[portalID] = control.Binding
	}
	return portals, approvalPortalIDs, approvalRevisions, bindings, s.portalCreationTemplateLocked(tenantID), nil
}

func (s *Service) projectApprovalDecisions(ctx context.Context, principal portalapi.Principal, portals []portalapi.Portal, portalIDs []string, revisions []portalapi.Revision) {
	if len(revisions) == 0 {
		return
	}
	decisions, decisionErr := s.approvalDecisions(ctx, principal, revisions)
	byPortal := make(map[string]approvalv2.Decision, len(portalIDs))
	for index, portalID := range portalIDs {
		if decisionErr != nil {
			byPortal[portalID] = approvalv2.Decision{
				Status: approvalv2.DecisionDenied, Profile: s.approvalBinding.Profile,
				Code: "approval.provider.unavailable", Message: "审批策略服务暂不可用",
			}
			continue
		}
		byPortal[portalID] = decisions[index]
	}
	for index := range portals {
		if decision, ok := byPortal[portals[index].ID]; ok {
			value := decision
			portals[index].PendingPublication.Approval = &value
		}
	}
}

func (s *Service) projectVersionControl(ctx context.Context, portals []portalapi.Portal, bindings map[string]PortalVersionControlBinding) {
	if len(bindings) == 0 {
		return
	}
	control, controlErr := versionControlFromContext(ctx)
	for index := range portals {
		binding, enabled := bindings[portals[index].ID]
		if !enabled {
			continue
		}
		if controlErr != nil {
			portals[index].VersionControl.Availability = portalapi.PortalVersionControlUnavailable
			continue
		}
		capabilities, err := control.Describe(ctx, binding, portals[index].ID)
		if err != nil {
			portals[index].VersionControl.Availability = portalapi.PortalVersionControlUnavailable
			continue
		}
		portals[index].VersionControl = portalVersionControlStatus(capabilities)
	}
}

func portalVersionControlStatus(capabilities PortalVersionControlCapabilities) portalapi.PortalVersionControlStatus {
	return portalapi.PortalVersionControlStatus{
		Enabled: true, Availability: portalapi.PortalVersionControlAvailable,
		Capabilities: portalVersionControlCapabilityNames(capabilities),
	}
}

func portalVersionControlCapabilityNames(capabilities PortalVersionControlCapabilities) []string {
	values := []string{"history"}
	if capabilities.Read {
		values = append(values, "read")
	}
	if capabilities.Diff {
		values = append(values, "diff")
	}
	if capabilities.Read && capabilities.Restore {
		values = append(values, "restore")
	}
	return values
}
