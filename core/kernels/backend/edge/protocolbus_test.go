package edge

import (
	"context"
	"encoding/json"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type recordingCatalog struct {
	tenant string
	spec   portalapi.PortalSpec
}

type recordingReferencePublisher struct {
	value pluginv1.ArtifactReferenceSnapshot
}

func (p *recordingReferencePublisher) Publish(_ context.Context, _ *contractv1.CallContext, value pluginv1.ArtifactReferenceSnapshot) error {
	p.value = value
	return nil
}

func (c *recordingCatalog) ValidatePortal(_ context.Context, tenant string, spec portalapi.PortalSpec) error {
	c.tenant, c.spec = tenant, spec
	return nil
}
func (c *recordingCatalog) MaterializePortal(_ context.Context, tenant string, spec portalapi.PortalSpec) ([]pluginv1.ArtifactReference, error) {
	c.tenant, c.spec = tenant, spec
	return []pluginv1.ArtifactReference{}, nil
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

func TestArtifactReferencePublicationServicePinsComposerIdentityAndOwnerNamespace(t *testing.T) {
	publisher := &recordingReferencePublisher{}
	service := ArtifactReferencePublicationService(publisher)
	snapshot, err := artifactreference.Seal(pluginv1.ArtifactReferenceSnapshot{
		OwnerKind: artifactreference.OwnerPortalActivation, OwnerID: "portal/admin", Generation: 1,
		References: []pluginv1.ArtifactReference{{Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.frontend.admin", Version: "1.0.0", Channel: "stable"}, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Purpose: "active"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(snapshot)
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: portalapi.ComposerPluginID}}
	result, _, err := service(context.Background(), call, raw)
	if err != nil || result == nil || publisher.value.Digest != snapshot.Digest {
		t.Fatalf("受信 Composer 引用未转发: result=%+v value=%+v err=%v", result, publisher.value, err)
	}
	call.Caller.Id = "cn.vastplan.product.fake"
	if _, _, err := service(context.Background(), call, raw); err == nil {
		t.Fatal("非 Composer 插件不得借用 Portal 引用发布器")
	}
	snapshot.OwnerID = "deployment/backend"
	raw, _ = json.Marshal(snapshot)
	call.Caller.Id = portalapi.ComposerPluginID
	if _, _, err := service(context.Background(), call, raw); err == nil {
		t.Fatal("Composer 不得写入 Portal owner 命名空间之外")
	}
}

func TestProtocolBusCapabilityClientRejectsNilHost(t *testing.T) {
	if _, err := NewProtocolBusCapabilityClient(nil); err == nil {
		t.Fatal("nil protocol host 必须拒绝")
	}
}
