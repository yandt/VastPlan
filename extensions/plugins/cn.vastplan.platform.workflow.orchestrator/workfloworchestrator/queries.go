package workfloworchestrator

import (
	"context"
	"sort"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
)

type CatalogFeature struct {
	Descriptor workflowv1.FeatureDescriptor    `json:"descriptor"`
	Owner      pluginv1.PluginArtifactIdentity `json:"owner"`
}

type CatalogTemplate struct {
	Descriptor workflowv1.NodeTemplateDescriptor `json:"descriptor"`
	Owner      pluginv1.PluginArtifactIdentity   `json:"owner"`
}

type CatalogProvider struct {
	Descriptor workflowv1.NodeProviderDescriptor `json:"descriptor"`
	Owner      pluginv1.PluginArtifactIdentity   `json:"owner"`
}

type CatalogSnapshot struct {
	Features  []CatalogFeature  `json:"features"`
	Templates []CatalogTemplate `json:"templates"`
	Providers []CatalogProvider `json:"providers"`
}

type PublishedDefinition struct {
	Definition  workflowv1.Definition    `json:"definition"`
	Ref         workflowv1.DefinitionRef `json:"ref"`
	PublishedBy string                   `json:"publishedBy"`
	PublishedAt time.Time                `json:"publishedAt"`
}

func (s *Service) ListCatalog(ctx context.Context, repository Repository, actor Actor) (CatalogSnapshot, error) {
	if actor.ID == "" {
		return CatalogSnapshot{}, ErrForbidden
	}
	result := CatalogSnapshot{Features: []CatalogFeature{}, Templates: []CatalogTemplate{}, Providers: []CatalogProvider{}}
	features, err := repository.List(ctx, kindFeature, "")
	if err != nil {
		return CatalogSnapshot{}, err
	}
	for _, record := range features {
		value, decodeErr := decodeDocument[featureRecord](record)
		if decodeErr != nil {
			return CatalogSnapshot{}, decodeErr
		}
		result.Features = append(result.Features, CatalogFeature{Descriptor: value.Descriptor, Owner: value.Owner})
	}
	templates, err := repository.List(ctx, kindNodeTemplate, "")
	if err != nil {
		return CatalogSnapshot{}, err
	}
	for _, record := range templates {
		value, decodeErr := decodeDocument[nodeTemplateRecord](record)
		if decodeErr != nil {
			return CatalogSnapshot{}, decodeErr
		}
		result.Templates = append(result.Templates, CatalogTemplate{Descriptor: value.Descriptor, Owner: value.Owner})
	}
	providers, err := repository.List(ctx, kindNodeProvider, "")
	if err != nil {
		return CatalogSnapshot{}, err
	}
	for _, record := range providers {
		value, decodeErr := decodeDocument[nodeProviderRecord](record)
		if decodeErr != nil {
			return CatalogSnapshot{}, decodeErr
		}
		result.Providers = append(result.Providers, CatalogProvider{Descriptor: value.Descriptor, Owner: value.Owner})
	}
	sort.Slice(result.Features, func(i, j int) bool { return result.Features[i].Descriptor.ID < result.Features[j].Descriptor.ID })
	sort.Slice(result.Templates, func(i, j int) bool { return result.Templates[i].Descriptor.ID < result.Templates[j].Descriptor.ID })
	sort.Slice(result.Providers, func(i, j int) bool { return result.Providers[i].Descriptor.ID < result.Providers[j].Descriptor.ID })
	return result, nil
}

func (s *Service) ListDefinitions(ctx context.Context, repository Repository, actor Actor) ([]PublishedDefinition, error) {
	if actor.ID == "" {
		return nil, ErrForbidden
	}
	records, err := repository.List(ctx, kindDefinition, "")
	if err != nil {
		return nil, err
	}
	result := make([]PublishedDefinition, 0, len(records))
	for _, record := range records {
		value, decodeErr := decodeDocument[definitionRecord](record)
		if decodeErr != nil {
			return nil, decodeErr
		}
		result = append(result, PublishedDefinition{Definition: value.Definition, Ref: workflowv1.DefinitionRef{ID: value.Definition.ID, Revision: value.Definition.Revision, Digest: value.Digest}, PublishedBy: value.PublishedBy, PublishedAt: value.PublishedAt.UTC()})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Definition.ID != result[j].Definition.ID {
			return result[i].Definition.ID < result[j].Definition.ID
		}
		return result[i].Definition.Revision > result[j].Definition.Revision
	})
	return result, nil
}

func (s *Service) ListBindings(ctx context.Context, repository Repository, actor Actor, serviceID string) ([]workflowv1.Binding, error) {
	if actor.ID == "" || !boundedText(serviceID, 160) {
		return nil, ErrForbidden
	}
	records, err := repository.List(ctx, kindBinding, "")
	if err != nil {
		return nil, err
	}
	result := make([]workflowv1.Binding, 0, len(records))
	for _, record := range records {
		if record.ServiceID != serviceID {
			continue
		}
		value, decodeErr := decodeDocument[workflowv1.Binding](record)
		if decodeErr != nil {
			return nil, decodeErr
		}
		value.Revision = record.Revision
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ServiceID+"\x00"+result[i].FeatureID < result[j].ServiceID+"\x00"+result[j].FeatureID
	})
	return result, nil
}

func (s *Service) ListInstances(ctx context.Context, repository Repository, actor Actor, serviceID string) ([]workflowv1.Instance, error) {
	if actor.ID == "" || !boundedText(serviceID, 160) {
		return nil, ErrForbidden
	}
	records, err := repository.List(ctx, kindInstance, "")
	if err != nil {
		return nil, err
	}
	result := make([]workflowv1.Instance, 0, len(records))
	for _, record := range records {
		if record.ServiceID != serviceID {
			continue
		}
		value, decodeErr := decodeDocument[instanceRecord](record)
		if decodeErr != nil {
			return nil, decodeErr
		}
		value.Instance.Revision = record.Revision
		result = append(result, value.Instance)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result, nil
}
