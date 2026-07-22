package pluginsettings

import (
	"context"
	"errors"
	"sort"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type DefinitionView struct {
	pluginconfiguration.Definition
	CatalogDigest string `json:"catalogDigest"`
}

func (s *Service) catalogs(ctx context.Context, host sdk.Host, call *contractv1.CallContext) ([]pluginconfiguration.Catalog, error) {
	if host == nil {
		return nil, errors.New("插件配置协调器缺少可信宿主")
	}
	operation := "list"
	target := &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: pluginconfiguration.KernelCatalogsService, Operation: &operation}
	result, raw, err := host.Call(ctx, target, call, []byte(`{}`))
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return nil, errors.New("可信部署配置目录不可用")
	}
	var response struct {
		Items []pluginconfiguration.Catalog `json:"items"`
	}
	if err := decodeStrict(raw, &response); err != nil {
		return nil, errors.New("可信部署配置目录响应无效")
	}
	for _, catalog := range response.Items {
		if err := catalog.Validate(); err != nil {
			return nil, errors.New("可信部署配置目录校验失败")
		}
	}
	return response.Items, nil
}

func flattenDefinitions(catalogs []pluginconfiguration.Catalog) []DefinitionView {
	items := make([]DefinitionView, 0)
	for _, catalog := range catalogs {
		for _, definition := range catalog.Items {
			items = append(items, DefinitionView{Definition: definition, CatalogDigest: catalog.Digest})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Deployment != items[j].Deployment {
			return items[i].Deployment < items[j].Deployment
		}
		if items[i].UnitID != items[j].UnitID {
			return items[i].UnitID < items[j].UnitID
		}
		return items[i].PluginID < items[j].PluginID
	})
	return items
}

func findDefinition(catalogs []pluginconfiguration.Catalog, id, digest string) (DefinitionView, error) {
	for _, catalog := range catalogs {
		for _, definition := range catalog.Items {
			if definition.ID != id {
				continue
			}
			if digest != "" && catalog.Digest != digest {
				return DefinitionView{}, ErrConflict
			}
			return DefinitionView{Definition: definition, CatalogDigest: catalog.Digest}, nil
		}
	}
	return DefinitionView{}, ErrNotFound
}
