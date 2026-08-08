package portalcomposer

import (
	"context"
	"encoding/json"
	"testing"

	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func TestPreparePortalPublicationReturnsStandardGovernedResource(t *testing.T) {
	service := newTestService(t)
	author := principal("author", "portal.compose")
	configuration, err := service.configurationFromCatalog(spec("/managed"), author.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	portal, err := service.CreatePortal(context.Background(), author, portalapi.CreatePortalRequest{PortalID: "managed", Configuration: configuration})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(struct {
		PortalID    string                                   `json:"portalId"`
		Publication portalapi.SubmitPortalPublicationRequest `json:"publication"`
	}{PortalID: portal.ID, Publication: portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: portal.WorkingCopy.Revision}})
	prepared, err := service.PreparePortalPublication(context.Background(), author, payload)
	if err != nil {
		t.Fatal(err)
	}
	var publication portalapi.PortalPublication
	if err := json.Unmarshal(prepared.Projection, &publication); err != nil {
		t.Fatal(err)
	}
	if publication.Status != portalapi.StatusPendingApproval || prepared.Digest != publication.Digest || prepared.Resource.Kind != portalapi.WorkflowPublicationResourceKind || prepared.Resource.ID == "" || prepared.Revision != int64(portal.WorkingCopy.Revision) {
		t.Fatalf("publication=%+v prepared=%+v", publication, prepared)
	}
	if err := workflowv1.ValidatePreparedResource(prepared, workflowv1.FeatureDescriptor{ID: portalapi.WorkflowPublicationFeatureID, Contract: "1.0.0", ResourceKind: portalapi.WorkflowPublicationResourceKind, DigestRequired: true, Actions: []workflowv1.ActionDescriptor{{ID: portalapi.WorkflowPublicationReleaseActionID, Capability: "platform.portal-composer", Operation: portalapi.ExecutePublicationReleaseOperation, Permission: "platform.portal.publish", Terminal: true}}}); err != nil {
		t.Fatal(err)
	}
}
