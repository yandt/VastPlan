package portalcomposer

import (
	"context"
	"errors"
	"testing"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func TestNavigationComposerOperationMapsStaleStateToStableConflict(t *testing.T) {
	for _, err := range []error{ErrInvalidState, ErrNotFound} {
		result := navigationComposerOperationError(portalapi.PrepareNavigationConfigurationOperation, err)
		if result.GetError().GetCode() != "portal.composer.conflict" || !result.GetError().GetRetryable() {
			t.Fatalf("navigation state error must be a retryable conflict: %+v", result)
		}
	}
	if result := navigationComposerOperationError("portalGovernance", errors.New("other")); result != nil {
		t.Fatalf("unrelated operation must retain its existing mapping: %+v", result)
	}
}

func TestNavigationConfigurationUsesHiddenCandidateAndPreservesWorkingCopy(t *testing.T) {
	service := newTestService(t)
	author, approver, publisher := principal("author"), principal("approver"), principal("publisher")
	configuration, err := service.configurationFromCatalog(spec("/"), author.TenantID)
	if err != nil || len(configuration.Services) == 0 {
		t.Fatalf("test configuration is incomplete: %v", err)
	}
	portal, err := service.CreatePortal(context.Background(), author, portalapi.CreatePortalRequest{PortalID: "operations", Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	publication, err := service.SubmitPortalPublication(context.Background(), author, portal.ID, portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: portal.WorkingCopy.Revision})
	if err != nil {
		t.Fatal(err)
	}
	publication, err = service.ApprovePortalPublication(withDifferentSubjectTestPolicy(context.Background()), approver, portal.ID, publication.ID, portalapi.PortalApprovalRequest{})
	if err != nil {
		t.Fatal(err)
	}
	publication, err = service.PublishPortalPublication(context.Background(), publisher, portal.ID, publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	release, err := service.ReleasePortalPublication(context.Background(), publisher, portal.ID, portalapi.PortalPublicationReleaseRequest{PublicationID: publication.ID})
	if err != nil {
		t.Fatal(err)
	}
	workingConfiguration := cloneJSON(configuration)
	workingConfiguration.Application.Route = "/next"
	if _, err := service.CreatePortalWorkingCopy(context.Background(), author, portal.ID, workingConfiguration); err != nil {
		t.Fatal(err)
	}

	serviceID := configuration.Services[0].ID
	trusted := portalapi.Principal{ID: "service-admin", TenantID: author.TenantID, System: true}
	request := portalapi.NavigationConfigurationRequest{
		CandidateID: "navigation-0123456789abcdef", PortalID: portal.ID, ServiceID: serviceID, ExpectedActivationID: release.ID,
		Folders: []frontendcompositionv1.NavigationFolder{{
			ID: "operations", ServiceID: serviceID, Label: "Operations",
			Members: []string{"cn.vastplan.example.first/root", "cn.vastplan.example.second/root"},
		}},
	}
	prepared, err := service.PrepareNavigationConfiguration(context.Background(), trusted, request)
	if err != nil || prepared.Status != portalapi.NavigationConfigurationPrepared || prepared.PreviousActivationID != release.ID {
		t.Fatalf("navigation preparation failed: %+v err=%v", prepared, err)
	}
	retried, err := service.PrepareNavigationConfiguration(context.Background(), trusted, request)
	if err != nil || retried.VersionID != prepared.VersionID {
		t.Fatalf("identical navigation preparation retry must be idempotent: %+v err=%v", retried, err)
	}
	conflicting := cloneJSON(request)
	conflicting.Folders[0].Label = "Different"
	if _, err := service.PrepareNavigationConfiguration(context.Background(), trusted, conflicting); err == nil {
		t.Fatal("same navigation candidate must reject different request content")
	}
	governance, err := service.PortalGovernance(context.Background(), author)
	if err != nil || governance.Portals[0].WorkingCopy == nil || governance.Portals[0].WorkingCopy.Configuration.Application.Route != "/next" {
		t.Fatalf("navigation candidate must not overwrite WorkingCopy: %+v err=%v", governance, err)
	}
	lookup := portalapi.NavigationConfigurationLookup{CandidateID: request.CandidateID, PortalID: portal.ID, ServiceID: serviceID}
	committed, err := service.CommitNavigationConfiguration(context.Background(), trusted, lookup)
	if err != nil || committed.Status != portalapi.NavigationConfigurationCommitted || committed.ActivationID == 0 {
		t.Fatalf("navigation commit failed: %+v err=%v", committed, err)
	}
	snapshot, err := service.ReadNavigationConfiguration(context.Background(), trusted, portal.ID, serviceID)
	if err != nil || snapshot.ActivationID != committed.ActivationID || len(snapshot.Folders) != 1 || snapshot.Folders[0].ID != "operations" {
		t.Fatalf("current navigation snapshot is wrong: %+v err=%v", snapshot, err)
	}
	rolledBack, err := service.RollbackNavigationConfiguration(context.Background(), trusted, lookup)
	if err != nil || rolledBack.Status != portalapi.NavigationConfigurationRolledBack {
		t.Fatalf("navigation rollback failed: %+v err=%v", rolledBack, err)
	}
}

func TestNavigationConfigurationRejectsCrossServiceFolders(t *testing.T) {
	service := New(acceptingCatalog{})
	_, err := service.PrepareNavigationConfiguration(context.Background(), portalapi.Principal{ID: "admin", TenantID: "tenant-a", System: true}, portalapi.NavigationConfigurationRequest{
		CandidateID: "navigation-0123456789abcdef", PortalID: "portal", ServiceID: "service-a", ExpectedActivationID: 1,
		Folders: []frontendcompositionv1.NavigationFolder{{ID: "folder", ServiceID: "service-b", Label: "Folder", Members: []string{"cn.example.a/root", "cn.example.b/root"}}},
	})
	if err == nil {
		t.Fatal("cross-service folder must be rejected before state access")
	}
}
