package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginmarketplace"
)

func TestListCatalogUsesConfiguredSourceAndBoundedQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/catalog/artifacts" || request.URL.Query().Get("target") != "backend" || request.URL.Query().Get("pageSize") != "20" {
			t.Fatalf("远端请求错误: %s", request.URL.String())
		}
		_ = json.NewEncoder(response).Encode(platformadminapi.ArtifactCatalogPage{Revision: 7, Total: 1, Page: 1, PageSize: 20, Items: []platformadminapi.ArtifactCatalogEntry{{
			Ref: artifactRef(), SHA256: sixtyFourA(), RepositoryRevision: 7, LifecycleStatus: "active",
		}}})
	}))
	defer server.Close()
	service, err := New(Config{Sources: []SourceConfig{{ID: "local", Label: "本地测试市场", URL: server.URL, AllowInsecureLoopback: true}}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := service.ListCatalog(context.Background(), nil, &contractv1.CallContext{TenantId: "local"}, pluginmarketplace.CatalogRequest{
		Version: 1, SourceID: "local", Query: platformadminapi.ArtifactCatalogQuery{Target: "backend", Page: 1, PageSize: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.Source.ID != "local" || page.Revision != 7 || len(page.Items) != 1 {
		t.Fatalf("目录投影错误: %#v", page)
	}
}

func TestListCatalogRejectsUnconfiguredSource(t *testing.T) {
	service, _ := New(Config{Sources: []SourceConfig{{ID: "market", Label: "市场", URL: "https://market.example"}}})
	_, err := service.ListCatalog(context.Background(), nil, &contractv1.CallContext{TenantId: "local"}, pluginmarketplace.CatalogRequest{Version: 1, SourceID: "attacker", Query: platformadminapi.ArtifactCatalogQuery{Page: 1, PageSize: 20}})
	if err == nil {
		t.Fatal("未配置市场必须拒绝")
	}
}

func TestListCatalogDoesNotFollowRedirectOutsideConfiguredSource(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalled = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL, http.StatusFound)
	}))
	defer source.Close()
	service, err := New(Config{Sources: []SourceConfig{{ID: "local", Label: "本地测试市场", URL: source.URL, AllowInsecureLoopback: true}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ListCatalog(context.Background(), nil, &contractv1.CallContext{TenantId: "local"}, pluginmarketplace.CatalogRequest{
		Version: 1, SourceID: "local", Query: platformadminapi.ArtifactCatalogQuery{Target: "backend", Page: 1, PageSize: 20},
	})
	if err == nil || targetCalled {
		t.Fatalf("Marketplace 不得跟随来源重定向: err=%v targetCalled=%t", err, targetCalled)
	}
}

func artifactRef() pluginv1.ArtifactRef {
	return pluginv1.ArtifactRef{PluginID: "cn.example.application", Version: "1.2.3", Channel: "stable"}
}
