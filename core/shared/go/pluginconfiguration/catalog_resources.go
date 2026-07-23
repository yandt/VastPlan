package pluginconfiguration

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

// ResourceCollection is a signed, browser-safe definition for independently
// versioned child configuration resources. Controller routing stays private on
// the parent Definition.
type ResourceCollection struct {
	ID                 string                            `json:"id"`
	Kind               string                            `json:"kind"`
	Title              string                            `json:"title"`
	Description        string                            `json:"description,omitempty"`
	Schema             json.RawMessage                   `json:"schema"`
	SchemaDigest       string                            `json:"schemaDigest"`
	ManagedCredentials []pluginv1.ManagedCredentialField `json:"managedCredentials,omitempty"`
	MinItems           uint32                            `json:"minItems,omitempty"`
	MaxItems           uint32                            `json:"maxItems"`
}

func configurationResourceCollectionsFor(manifest pluginv1.Manifest) ([]ResourceCollection, error) {
	if manifest.Configuration == nil || len(manifest.Configuration.ResourceCollections) == 0 {
		return nil, nil
	}
	collections := make([]ResourceCollection, 0, len(manifest.Configuration.ResourceCollections))
	for _, declared := range manifest.Configuration.ResourceCollections {
		id, err := pluginv1.ConfigurationResourceCollectionID(manifest.ID, declared.ID)
		if err != nil {
			return nil, err
		}
		schema := append(json.RawMessage(nil), declared.Schema...)
		digest, err := digestRawJSON(schema)
		if err != nil {
			return nil, err
		}
		collections = append(collections, ResourceCollection{
			ID: id, Kind: declared.Kind, Title: declared.Title, Description: declared.Description,
			Schema: schema, SchemaDigest: digest,
			ManagedCredentials: append([]pluginv1.ManagedCredentialField(nil), declared.ManagedCredentials...),
			MinItems:           declared.MinItems, MaxItems: declared.MaxItems,
		})
	}
	sort.Slice(collections, func(i, j int) bool { return collections[i].ID < collections[j].ID })
	return collections, nil
}

func validateResourceCollections(item Definition) error {
	if err := validateResourceControllerTarget(item); err != nil {
		return err
	}
	if (item.ResourceController == nil) != (len(item.ResourceCollections) == 0) {
		return errors.New("resource controller 与集合不完整")
	}
	seenCollections := map[string]struct{}{}
	for _, collection := range item.ResourceCollections {
		if _, duplicate := seenCollections[collection.ID]; duplicate {
			return fmt.Errorf("资源集合重复: %s", collection.ID)
		}
		seenCollections[collection.ID] = struct{}{}
		if !strings.HasPrefix(collection.ID, "cfgc_") || len(collection.ID) != 29 || collection.Kind != "profile" ||
			strings.TrimSpace(collection.Title) == "" || collection.MaxItems == 0 || collection.MaxItems > 256 || collection.MinItems > collection.MaxItems ||
			len(collection.SchemaDigest) != 64 || !json.Valid(collection.Schema) {
			return fmt.Errorf("资源集合内容无效: %s", collection.ID)
		}
		if expected, err := digestRawJSON(collection.Schema); err != nil || expected != collection.SchemaDigest {
			return fmt.Errorf("资源集合 Schema 摘要无效: %s", collection.ID)
		}
		fieldIDs, purposes := map[string]struct{}{}, map[string]struct{}{}
		for _, field := range collection.ManagedCredentials {
			if _, duplicate := fieldIDs[field.ID]; duplicate {
				return fmt.Errorf("资源集合凭证字段重复: %s", collection.ID)
			}
			if _, duplicate := purposes[field.Purpose]; duplicate {
				return fmt.Errorf("资源集合凭证用途重复: %s", collection.ID)
			}
			fieldIDs[field.ID], purposes[field.Purpose] = struct{}{}, struct{}{}
		}
	}
	return nil
}
