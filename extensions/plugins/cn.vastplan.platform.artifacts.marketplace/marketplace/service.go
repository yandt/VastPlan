package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginmarketplace"
	credentialmaterial "cdsoft.com.cn/VastPlan/extensions/sdk/go/credentialmaterial"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Service struct {
	sources map[string]SourceConfig
	ordered []SourceConfig
	client  catalogClient
	now     func() time.Time
}

func New(config Config) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	ordered := config.normalized()
	sources := make(map[string]SourceConfig, len(ordered))
	for _, source := range ordered {
		sources[source.ID] = source
	}
	return &Service{sources: sources, ordered: ordered, client: newHTTPCatalogClient(), now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Service) Sources() pluginmarketplace.ListSourcesResult {
	result := pluginmarketplace.ListSourcesResult{Version: pluginmarketplace.ProtocolVersion, Sources: make([]pluginmarketplace.Source, 0, len(s.ordered))}
	for _, source := range s.ordered {
		result.Sources = append(result.Sources, source.Public())
	}
	return result
}

func (s *Service) ListCatalog(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request pluginmarketplace.CatalogRequest) (pluginmarketplace.CatalogPage, error) {
	if request.Version != pluginmarketplace.ProtocolVersion {
		return pluginmarketplace.CatalogPage{}, errors.New("Marketplace 协议版本无效")
	}
	source, ok := s.sources[request.SourceID]
	if !ok {
		return pluginmarketplace.CatalogPage{}, errors.New("Marketplace source 不存在")
	}
	query, err := normalizeQuery(request.Query)
	if err != nil {
		return pluginmarketplace.CatalogPage{}, err
	}
	var page platformadminapi.ArtifactCatalogPage
	if strings.HasPrefix(source.URL, "vastplan://") {
		page, err = listInternalCatalog(ctx, host, call, query)
	} else if source.CredentialRef == nil {
		page, err = s.client.List(ctx, source, query, "")
	} else {
		if call == nil || call.GetTenantId() == "" {
			return pluginmarketplace.CatalogPage{}, errors.New("Marketplace 调用缺少租户")
		}
		material, materialErr := credentialmaterial.NewFromEnvironment(host, call.GetTenantId(), *source.CredentialRef)
		if materialErr != nil {
			return pluginmarketplace.CatalogPage{}, materialErr
		}
		err = material.WithMaterial(ctx, s.now(), func(secret credentialmaterial.Material) error {
			page, materialErr = s.client.List(ctx, source, query, string(secret.Bytes()))
			return materialErr
		})
	}
	if err != nil {
		return pluginmarketplace.CatalogPage{}, err
	}
	result := pluginmarketplace.CatalogPage{Version: pluginmarketplace.ProtocolVersion, Source: source.Public(), ArtifactCatalogPage: page}
	if err := validateCatalogPage(result, query); err != nil {
		return pluginmarketplace.CatalogPage{}, err
	}
	sort.SliceStable(result.Items, func(i, j int) bool {
		if result.Items[i].Ref.PluginID == result.Items[j].Ref.PluginID {
			return result.Items[i].Ref.Version > result.Items[j].Ref.Version
		}
		return result.Items[i].Ref.PluginID < result.Items[j].Ref.PluginID
	})
	return result, nil
}

func listInternalCatalog(ctx context.Context, host sdk.Host, call *contractv1.CallContext, query platformadminapi.ArtifactCatalogQuery) (platformadminapi.ArtifactCatalogPage, error) {
	if host == nil || call == nil {
		return platformadminapi.ArtifactCatalogPage{}, errors.New("内部 Marketplace Catalog 缺少可信宿主")
	}
	operation, logicalService, routingDomain := "listCatalog", "platform.artifacts.repository", "platform"
	raw, err := json.Marshal(query)
	if err != nil {
		return platformadminapi.ArtifactCatalogPage{}, err
	}
	result, response, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: platformadminapi.ArtifactsCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}, call, raw)
	if err != nil || result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		return platformadminapi.ArtifactCatalogPage{}, errors.New("内部 Artifact Catalog 暂不可用")
	}
	var page platformadminapi.ArtifactCatalogPage
	if err := json.Unmarshal(response, &page); err != nil {
		return page, errors.New("内部 Artifact Catalog 响应无效")
	}
	return page, nil
}
