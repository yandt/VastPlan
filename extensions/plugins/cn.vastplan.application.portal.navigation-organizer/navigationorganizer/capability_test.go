package navigationorganizer

import (
	"context"
	"encoding/json"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

type organizerHost struct {
	request portalapi.NavigationConfigurationRequest
}

func (h *organizerHost) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	switch target.GetOperation() {
	case portalapi.PrepareNavigationConfigurationOperation:
		if err := json.Unmarshal(payload, &h.request); err != nil {
			return nil, nil, err
		}
		prepared := portalapi.NavigationConfigurationPreparation{
			CandidateID: h.request.CandidateID, PortalID: h.request.PortalID, ServiceID: h.request.ServiceID,
			Status: portalapi.NavigationConfigurationPrepared, VersionID: 2, PreviousActivationID: h.request.ExpectedActivationID,
		}
		raw, _ := json.Marshal(prepared)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	case portalapi.CommitNavigationConfigurationOperation:
		prepared := portalapi.NavigationConfigurationPreparation{
			CandidateID: h.request.CandidateID, PortalID: h.request.PortalID, ServiceID: h.request.ServiceID,
			Status: portalapi.NavigationConfigurationCommitted, VersionID: 2, PreviousActivationID: h.request.ExpectedActivationID, ActivationID: 3,
		}
		raw, _ := json.Marshal(prepared)
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	default:
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "unexpected"}}, nil, nil
	}
}

func TestPublishUsesTrustedManagementTargetForPortalAndService(t *testing.T) {
	host := &organizerHost{}
	payload := []byte("{\"schemaVersion\":\"v1\",\"routeId\":\"portal.navigation.publish\",\"method\":\"PUT\",\"pathParams\":{},\"query\":{},\"body\":{\"candidateId\":\"navigation-0123456789\",\"expectedActivationId\":7,\"folders\":[{\"id\":\"operations\",\"label\":\"Operations\",\"members\":[\"cn.example.a/root\",\"cn.example.b/root\"]}]},\"managementTarget\":{\"portalId\":\"operations\",\"serviceId\":\"service-a\",\"activationId\":7,\"generation\":7}}")
	handler := Contribution().Handlers["apiPublish"]
	result, _, err := handler(context.Background(), host, &contractv1.CallContext{TenantId: "tenant-a"}, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("publish failed: result=%+v err=%v", result, err)
	}
	if host.request.PortalID != "operations" || host.request.ServiceID != "service-a" || host.request.ExpectedActivationID != 7 || len(host.request.Folders) != 1 || host.request.Folders[0].ServiceID != "service-a" {
		t.Fatalf("trusted management scope was not projected: %+v", host.request)
	}
}

func TestPublishRejectsMissingManagementTarget(t *testing.T) {
	payload := []byte("{\"schemaVersion\":\"v1\",\"routeId\":\"portal.navigation.publish\",\"method\":\"PUT\",\"pathParams\":{},\"query\":{},\"body\":{}}")
	handler := Contribution().Handlers["apiPublish"]
	result, _, err := handler(context.Background(), &organizerHost{}, &contractv1.CallContext{TenantId: "tenant-a"}, payload)
	if err != nil || result.GetError().GetCode() != "portal.navigation.invalid" {
		t.Fatalf("missing trusted target must be rejected: result=%+v err=%v", result, err)
	}
}

func TestPublishSeparatesMalformedRequestFromActivationConflict(t *testing.T) {
	handler := Contribution().Handlers["apiPublish"]
	malformed := []byte("{\"schemaVersion\":\"v1\",\"routeId\":\"portal.navigation.publish\",\"method\":\"PUT\",\"pathParams\":{},\"query\":{},\"body\":{},\"managementTarget\":{\"portalId\":\"operations\",\"serviceId\":\"service-a\",\"activationId\":7,\"generation\":7}}")
	result, _, err := handler(context.Background(), &organizerHost{}, &contractv1.CallContext{TenantId: "tenant-a"}, malformed)
	if err != nil || result.GetError().GetCode() != "portal.navigation.invalid" {
		t.Fatalf("malformed publication must be invalid: result=%+v err=%v", result, err)
	}
	stale := []byte("{\"schemaVersion\":\"v1\",\"routeId\":\"portal.navigation.publish\",\"method\":\"PUT\",\"pathParams\":{},\"query\":{},\"body\":{\"candidateId\":\"navigation-0123456789\",\"expectedActivationId\":6,\"folders\":[]},\"managementTarget\":{\"portalId\":\"operations\",\"serviceId\":\"service-a\",\"activationId\":7,\"generation\":7}}")
	result, _, err = handler(context.Background(), &organizerHost{}, &contractv1.CallContext{TenantId: "tenant-a"}, stale)
	if err != nil || result.GetError().GetCode() != "portal.navigation.conflict" {
		t.Fatalf("stale publication must be a conflict: result=%+v err=%v", result, err)
	}
}
