package edge

import (
	"context"
	"encoding/json"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/portalapi"
)

type recordingCatalog struct {
	tenant string
	spec   portalapi.PortalSpec
}

func (c *recordingCatalog) ValidatePortal(_ context.Context, tenant string, spec portalapi.PortalSpec) error {
	c.tenant, c.spec = tenant, spec
	return nil
}

func TestCatalogValidationServiceOnlyAcceptsPluginForDelegatedTenant(t *testing.T) {
	catalog := &recordingCatalog{}
	service := CatalogValidationService(catalog)
	payload, _ := json.Marshal(catalogValidationRequest{TenantID: "tenant-a", Spec: portalapi.PortalSpec{ID: "admin"}})
	result, raw, err := service(context.Background(), &contractv1.CallContext{
		TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "composer"},
	}, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK || string(raw) != `{"valid":true}` || catalog.tenant != "tenant-a" || catalog.spec.ID != "admin" {
		t.Fatalf("目录校验调用错误: result=%+v raw=%s catalog=%+v err=%v", result, raw, catalog, err)
	}
	_, _, err = service(context.Background(), &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "attacker"}}, payload)
	if err == nil {
		t.Fatal("非插件调用必须拒绝")
	}
	payload, _ = json.Marshal(catalogValidationRequest{TenantID: "tenant-b", Spec: portalapi.PortalSpec{ID: "admin"}})
	_, _, err = service(context.Background(), &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "composer"}}, payload)
	if err == nil || catalog.tenant == "tenant-b" {
		t.Fatal("插件不可替换委托 tenant")
	}
}

func TestProtocolBusCapabilityClientRejectsNilHost(t *testing.T) {
	if _, err := NewProtocolBusCapabilityClient(nil); err == nil {
		t.Fatal("nil protocol host 必须拒绝")
	}
}
