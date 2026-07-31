package versionledger

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func TestServiceRoutesByTrustedNamespaceAndProjectsActor(t *testing.T) {
	primary := NewMemoryProvider()
	portal := NewMemoryProvider()
	registry := NewRegistry()
	if err := registry.Register("primary", primary); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("portal", portal); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "primary", []ProviderRoute{{Namespace: "portal.configuration", Provider: "portal"}})
	if err != nil {
		t.Fatal(err)
	}
	request := versioningv1.PutVersionRequest{
		Stream:         versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "portal-main"},
		IdempotencyKey: "portal-main:revision:0001", Content: json.RawMessage(`{"layout":"standard"}`),
		Message: "Submit Portal publication", Labels: map[string]string{"domain": "portal", "portal.id": "portal-main"},
	}
	call := pluginCall("tenant-a")
	result, raw := invokeService(t, service, versioningv1.OperationPutVersion, call, request)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("写入失败: %+v", result)
	}
	parsed, err := versioningv1.ParseResult(versioningv1.OperationPutVersion, raw)
	if err != nil {
		t.Fatal(err)
	}
	version := parsed.(*versioningv1.PutVersionResult).Version
	if version.ActorID != "plugin:cn.vastplan.platform.configuration.portal-composer" {
		t.Fatalf("actor 未从可信 CallContext 投影: %s", version.ActorID)
	}
	if version.Labels["domain"] != "portal" || version.Labels["portal.id"] != "portal-main" {
		t.Fatalf("Provider 返回记录丢失领域标签: %+v", version.Labels)
	}
	if _, err := portal.GetVersion(context.Background(), Scope{TenantID: "tenant-a"}, versioningv1.GetVersionRequest{Ref: version.Ref}); err != nil {
		t.Fatalf("namespace 路由未写入 portal Provider: %v", err)
	}
	if _, err := primary.GetVersion(context.Background(), Scope{TenantID: "tenant-a"}, versioningv1.GetVersionRequest{Ref: version.Ref}); errorCode(err) != versioningv1.ErrorNotFound {
		t.Fatalf("namespace 路由不应写入默认 Provider: %v", err)
	}
}

func TestServiceRejectsDirectUserAndInvalidConfiguration(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("primary", NewMemoryProvider()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(registry, "missing", nil); err == nil {
		t.Fatal("默认 Provider 必须已注册")
	}
	if _, err := NewService(registry, "primary", []ProviderRoute{{Namespace: "portal.configuration", Provider: "missing"}}); err == nil {
		t.Fatal("route 必须引用已注册 Provider")
	}
	service, err := NewService(registry, "primary", nil)
	if err != nil {
		t.Fatal(err)
	}
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "user-a"}}
	result, _ := invokeService(t, service, versioningv1.OperationProviders, call, versioningv1.ProviderListRequest{})
	if result.GetStatus() != contractv1.CallResult_STATUS_ERROR || result.GetError().GetCode() != versioningv1.ErrorInvalidRequest {
		t.Fatalf("用户不得直接调用基础 Version Ledger: %+v", result)
	}
}

func TestServiceContributionExposesCompleteProtocol(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("primary", NewMemoryProvider()); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "primary", nil)
	if err != nil {
		t.Fatal(err)
	}
	contribution := service.Contribution()
	if contribution.ID != versioningv1.LedgerCapability || !json.Valid(contribution.Descriptor) || len(contribution.Handlers) != 15 {
		t.Fatalf("Version Ledger contribution 无效: %+v", contribution)
	}
	for _, operation := range []string{
		versioningv1.OperationProviders, versioningv1.OperationPutVersion, versioningv1.OperationGetVersion,
		versioningv1.OperationListHistory, versioningv1.OperationGetHead, versioningv1.OperationMoveHead,
		versioningv1.OperationListHeads, versioningv1.OperationCreateHead, versioningv1.OperationDeleteHead,
		versioningv1.OperationCreateTag, versioningv1.OperationGetTag, versioningv1.OperationListTags,
		versioningv1.OperationCompare, versioningv1.OperationIsAncestor, versioningv1.OperationCommonAncestor,
	} {
		if contribution.Handlers[operation] == nil {
			t.Fatalf("Version Ledger 缺少 %s handler", operation)
		}
	}
}

type wrongVersionProvider struct{ *MemoryProvider }

func (p wrongVersionProvider) GetVersion(ctx context.Context, scope Scope, request versioningv1.GetVersionRequest) (versioningv1.GetVersionResult, error) {
	result, err := p.MemoryProvider.GetVersion(ctx, scope, request)
	if err == nil {
		result.Version.Ref.VersionID = strings.Repeat("f", 64)
	}
	return result, err
}

func TestServiceRejectsValidButMisroutedProviderResponse(t *testing.T) {
	provider := wrongVersionProvider{MemoryProvider: NewMemoryProvider()}
	registry := NewRegistry()
	if err := registry.Register("primary", provider); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "primary", nil)
	if err != nil {
		t.Fatal(err)
	}
	call := pluginCall("tenant-a")
	put := versioningv1.PutVersionRequest{
		Stream:         versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "portal-main"},
		IdempotencyKey: "portal-main:revision:0001", Content: json.RawMessage(`{}`),
	}
	result, raw := invokeService(t, service, versioningv1.OperationPutVersion, call, put)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	parsed, err := versioningv1.ParseResult(versioningv1.OperationPutVersion, raw)
	if err != nil {
		t.Fatal(err)
	}
	ref := parsed.(*versioningv1.PutVersionResult).Version.Ref
	result, _ = invokeService(t, service, versioningv1.OperationGetVersion, call, versioningv1.GetVersionRequest{Ref: ref})
	if result.GetStatus() != contractv1.CallResult_STATUS_ERROR || result.GetError().GetCode() != versioningv1.ErrorCorrupted {
		t.Fatalf("答非所问的 Provider 响应必须 fail-closed: %+v", result)
	}
}

func TestServiceProvidesCompareAncestryHeadsAndTags(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("primary", NewMemoryProvider()); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(registry, "primary", nil)
	if err != nil {
		t.Fatal(err)
	}
	call := pluginCall("tenant-a")
	root := putViaService(t, service, call, "graph:revision:0001", nil, `{"a":1,"array":[1],"removed":true}`)
	left := putViaService(t, service, call, "graph:revision:0002", []versioningv1.VersionRef{root.Ref}, `{"a":2,"array":[1,2]}`)
	right := putViaService(t, service, call, "graph:revision:0003", []versioningv1.VersionRef{root.Ref}, `{"a":1,"added":"yes","array":[1]}`)
	merge := putViaService(t, service, call, "graph:revision:0004", []versioningv1.VersionRef{left.Ref, right.Ref}, `{"a":2,"added":"yes","array":[1,2]}`)
	if len(merge.Parents) != 2 {
		t.Fatalf("Ledger 未保存双父版本: %+v", merge.Parents)
	}

	result, raw := invokeService(t, service, versioningv1.OperationCompare, call, versioningv1.CompareVersionsRequest{Left: root.Ref, Right: left.Ref})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("比较版本失败: %+v", result)
	}
	parsed, err := versioningv1.ParseResult(versioningv1.OperationCompare, raw)
	if err != nil {
		t.Fatal(err)
	}
	comparison := parsed.(*versioningv1.CompareVersionsResult)
	if comparison.Summary.Replaced != 2 || comparison.Summary.Removed != 1 || comparison.Summary.Total != 3 || comparison.Patch[0].Path != "/a" || comparison.Patch[1].Path != "/array" || comparison.Patch[2].Path != "/removed" {
		t.Fatalf("JSON Patch 不稳定或统计错误: %+v", comparison)
	}

	result, raw = invokeService(t, service, versioningv1.OperationIsAncestor, call, versioningv1.IsAncestorRequest{Ancestor: root.Ref, Descendant: merge.Ref})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	ancestor := parseResult[versioningv1.IsAncestorResult](t, versioningv1.OperationIsAncestor, raw)
	if !ancestor.IsAncestor || ancestor.Distance != 2 {
		t.Fatalf("DAG 祖先距离错误: %+v", ancestor)
	}
	result, raw = invokeService(t, service, versioningv1.OperationCommonAncestor, call, versioningv1.FindCommonAncestorRequest{Left: left.Ref, Right: right.Ref})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	common := parseResult[versioningv1.FindCommonAncestorResult](t, versioningv1.OperationCommonAncestor, raw)
	if !common.Found || common.Ancestor == nil || *common.Ancestor != root.Ref || common.LeftDistance != 1 || common.RightDistance != 1 {
		t.Fatalf("最近共同祖先错误: %+v", common)
	}

	result, raw = invokeService(t, service, versioningv1.OperationCreateHead, call, versioningv1.CreateHeadRequest{Stream: root.Ref.Stream, Name: "feature", Target: left.Ref})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	head := parseResult[versioningv1.CreateHeadResult](t, versioningv1.OperationCreateHead, raw)
	if head.Head.Target != left.Ref || head.Head.Revision != 1 {
		t.Fatalf("创建 Head 结果错误: %+v", head)
	}
	result, _ = invokeService(t, service, versioningv1.OperationDeleteHead, call, versioningv1.DeleteHeadRequest{Stream: root.Ref.Stream, Name: "feature", ExpectedRevision: 1})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("删除 Head 失败: %+v", result)
	}
	result, raw = invokeService(t, service, versioningv1.OperationCreateHead, call, versioningv1.CreateHeadRequest{Stream: root.Ref.Stream, Name: "feature", Target: merge.Ref})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("重建 Head 失败: %+v", result)
	}
	recreatedHead := parseResult[versioningv1.CreateHeadResult](t, versioningv1.OperationCreateHead, raw)
	if recreatedHead.Head.Revision != 2 {
		t.Fatalf("Service 不得拒绝延续修订号的重建 Head: %+v", recreatedHead)
	}
	result, raw = invokeService(t, service, versioningv1.OperationCreateTag, call, versioningv1.CreateTagRequest{Stream: root.Ref.Stream, Name: "release-1", Target: merge.Ref})
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatal(result)
	}
	tag := parseResult[versioningv1.CreateTagResult](t, versioningv1.OperationCreateTag, raw)
	if tag.Tag.Target != merge.Ref || tag.Tag.ActorID != "plugin:cn.vastplan.platform.configuration.portal-composer" {
		t.Fatalf("Tag 未绑定可信 actor: %+v", tag)
	}
}

func putViaService(t *testing.T, service *Service, call *contractv1.CallContext, key string, parents []versioningv1.VersionRef, content string) versioningv1.VersionRecord {
	t.Helper()
	request := versioningv1.PutVersionRequest{
		Stream:         versioningv1.StreamKey{Namespace: "portal.configuration", StreamID: "portal-main"},
		IdempotencyKey: key, Parents: parents, Content: json.RawMessage(content),
	}
	result, raw := invokeService(t, service, versioningv1.OperationPutVersion, call, request)
	if result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("写入版本失败: %+v", result)
	}
	return parseResult[versioningv1.PutVersionResult](t, versioningv1.OperationPutVersion, raw).Version
}

func parseResult[T any](t *testing.T, operation string, raw []byte) T {
	t.Helper()
	parsed, err := versioningv1.ParseResult(operation, raw)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := parsed.(*T)
	if !ok {
		t.Fatalf("%s 返回类型错误: %T", operation, parsed)
	}
	return *value
}

func pluginCall(tenant string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: tenant, Caller: &contractv1.Caller{
		Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.platform.configuration.portal-composer",
	}}
}

func invokeService(t *testing.T, service *Service, operation string, call *contractv1.CallContext, request any) (*contractv1.CallResult, []byte) {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	result, payload, err := service.Contribution().Handlers[operation](context.Background(), nil, call, raw)
	if err != nil {
		t.Fatal(err)
	}
	return result, payload
}
