package portalcomposer

import (
	"context"
	"encoding/json"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func TestPreferenceContributionRequiresPortalBFFAndDerivesSubject(t *testing.T) {
	service := New(nil)
	host := newStateOnlyHost(t)
	contribution := PreferenceContribution(service)
	handler := contribution.Handlers["put"]
	payload, _ := json.Marshal(portalapi.PutPortalPreferenceRequest{Scope: testPreferenceScope(), Values: portalapi.PortalPreferenceValues{RendererID: "mui"}})
	untrusted := &contractv1.CallContext{TenantId: "tenant-a", Scene: "other", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "mallory"}, Principal: &contractv1.Principal{UserId: "alice"}}
	result, _, err := handler(context.Background(), host, untrusted, payload)
	if err != nil || result.GetError().GetCode() != "permission.denied" {
		t.Fatalf("untrusted scene must be denied: result=%+v err=%v", result, err)
	}
	trusted := &contractv1.CallContext{TenantId: "tenant-a", Scene: "portal.bff", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "alice"}, Principal: &contractv1.Principal{UserId: "alice"}}
	result, raw, err := handler(context.Background(), host, trusted, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("trusted put failed: result=%+v err=%v", result, err)
	}
	var saved portalapi.PortalPreference
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Revision != 1 || saved.Values.RendererID != "mui" {
		t.Fatalf("unexpected response: value=%+v err=%v", saved, err)
	}
}

func TestPreferenceContributionReturnsStableConflict(t *testing.T) {
	service := New(nil)
	host := newStateOnlyHost(t)
	handler := PreferenceContribution(service).Handlers["put"]
	callContext := &contractv1.CallContext{TenantId: "tenant-a", Scene: "portal.bff", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "alice"}, Principal: &contractv1.Principal{UserId: "alice"}}
	first, _ := json.Marshal(portalapi.PutPortalPreferenceRequest{Scope: testPreferenceScope(), Values: portalapi.PortalPreferenceValues{RendererID: "mui"}})
	if result, _, err := handler(context.Background(), host, callContext, first); err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("initial put failed: result=%+v err=%v", result, err)
	}
	stale, _ := json.Marshal(portalapi.PutPortalPreferenceRequest{Scope: testPreferenceScope(), Values: portalapi.PortalPreferenceValues{RendererID: "arco"}})
	result, _, err := handler(context.Background(), host, callContext, stale)
	if err != nil || result.GetError().GetCode() != preferenceConflictCode {
		t.Fatalf("conflict code mismatch: result=%+v err=%v", result, err)
	}
}
