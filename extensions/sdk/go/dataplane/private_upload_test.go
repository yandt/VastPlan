package dataplane

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
)

type grantCaller struct {
	call *contractv1.CallContext
	req  TicketRequest
}

func (c *grantCaller) Call(_ context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
	c.call = call
	_ = json.Unmarshal(raw, &c.req)
	grant, _ := json.Marshal(Grant{Endpoint: "https://content.internal:9445", LeaseID: "lease_demo", Ticket: "a" + strings.Repeat("b", 42), ExpiresAt: time.Now().Add(30 * time.Second)})
	if target.GetCapability() != "platform.api-exposure" || target.GetOperation() != privateTicketOperation {
		return nil, nil, context.Canceled
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, grant, nil
}

func TestIssuePrivateGrantUsesTrustedCapability(t *testing.T) {
	caller := &grantCaller{}
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_RUNNER, Id: "desktop-a"}, Principal: &contractv1.Principal{UserId: "alice"}}
	request := TicketRequest{DataPlaneExposureID: "dpx_aaaaaaaaaaaaaaaaaaaa", Method: "PUT", Resource: "/v1/uploads/stg_abcdefghijklmnop", ContentSHA256: strings.Repeat("d", 64)}
	grant, err := IssuePrivateGrant(context.Background(), caller, call, request)
	if err != nil || grant.Endpoint == "" || caller.call != call || caller.req != request {
		t.Fatalf("grant=%+v request=%+v err=%v", grant, caller.req, err)
	}
}
