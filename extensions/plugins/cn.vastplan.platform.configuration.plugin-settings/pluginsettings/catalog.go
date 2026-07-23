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
	CatalogDigest               string `json:"catalogDigest"`
	ControllerAvailable         bool   `json:"controllerAvailable"`
	ResourceControllerAvailable bool   `json:"resourceControllerAvailable"`
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

func (s *Service) publicDefinitions(tenant string, catalogs []pluginconfiguration.Catalog) []DefinitionView {
	overlays := s.hotValueOverlays(tenant)
	items := make([]DefinitionView, 0)
	for _, catalog := range catalogs {
		for _, definition := range catalog.Items {
			if candidate, ok := overlays[definition.ID]; ok && candidate.CatalogDigest == catalog.Digest && candidate.SchemaDigest == definition.SchemaDigest && candidate.ArtifactSHA256 == definition.Artifact.SHA256 {
				definition.Values = append([]byte(nil), candidate.Values...)
			}
			items = append(items, publicDefinitionView(definition, catalog.Digest))
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

func (s *Service) publicDefinition(tenant string, view DefinitionView) DefinitionView {
	if candidate, ok := s.hotValueOverlays(tenant)[view.ID]; ok && candidate.CatalogDigest == view.CatalogDigest && candidate.SchemaDigest == view.SchemaDigest && candidate.ArtifactSHA256 == view.Artifact.SHA256 {
		view.Values = append([]byte(nil), candidate.Values...)
	}
	return publicDefinitionView(view.Definition, view.CatalogDigest)
}

func (s *Service) hotValueOverlays(tenant string) map[string]pluginconfiguration.Candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	overlays := map[string]pluginconfiguration.Candidate{}
	for id, record := range state.HotActivations {
		candidate, ok := state.Candidates[id]
		if !ok || record.Status != hotReady || candidate.Status != pluginconfiguration.CandidateReady || candidate.ApplyPath != pluginconfiguration.ApplyHotService {
			continue
		}
		current, exists := overlays[candidate.ConfigurationID]
		if !exists || candidate.UpdatedAt > current.UpdatedAt {
			overlays[candidate.ConfigurationID] = cloneCandidate(candidate)
		}
	}
	return overlays
}

func publicDefinitionView(definition pluginconfiguration.Definition, catalogDigest string) DefinitionView {
	// 0.8.0 deliberately exposes the first hot-service pilot only for
	// definitions without managed credentials. Retained/new reference merge
	// semantics must be completed before the UI can safely offer that path.
	available := definition.Controller != nil && len(definition.ManagedCredentials) == 0
	resourceAvailable := definition.ResourceController != nil && len(definition.ResourceCollections) > 0
	definition.Controller = nil
	definition.ResourceController = nil
	return DefinitionView{Definition: definition, CatalogDigest: catalogDigest, ControllerAvailable: available, ResourceControllerAvailable: resourceAvailable}
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
