package edge

import (
	"context"
	"encoding/json"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type recordingClient struct {
	principal portalapi.Principal
	operation string
	payload   json.RawMessage
}

func (c *recordingClient) Call(_ context.Context, p portalapi.Principal, op string, payload []byte) ([]byte, error) {
	c.principal = p
	c.operation = op
	c.payload = append([]byte(nil), payload...)
	return []byte(`{"id":7,"tenantId":"tenant-a","portalId":"admin","status":"Draft"}`), nil
}

func TestCapabilityServiceForwardsOnlyVerifiedPrincipalAndOperation(t *testing.T) {
	c := &recordingClient{}
	s, err := NewCapabilityService(c)
	if err != nil {
		t.Fatal(err)
	}
	p := portalapi.Principal{ID: "verified", TenantID: "tenant-a", Roles: []string{"portal.compose"}}
	_, err = s.CreateDraft(context.Background(), p, frontendcompositionv1.ApplicationComposition{Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "admin"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend}, Route: "/", Plugins: []frontendcompositionv1.PluginRef{}})
	if err != nil {
		t.Fatal(err)
	}
	if c.operation != "createDraft" || c.principal.ID != "verified" || c.principal.TenantID != "tenant-a" {
		t.Fatalf("capability 调用未保留宿主身份: op=%s principal=%+v", c.operation, c.principal)
	}
	var got frontendcompositionv1.ApplicationComposition
	if err := json.Unmarshal(c.payload, &got); err != nil || got.ID != "admin" {
		t.Fatalf("草稿负载错误: %s %v", c.payload, err)
	}
}
