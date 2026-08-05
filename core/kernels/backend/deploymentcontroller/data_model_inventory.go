package deploymentcontroller

import (
	"encoding/json"
	"fmt"
	"sort"

	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/datamodelinventory"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
)

const maxDataModelInventoryBytes = datamodelinventory.MaxInventoryBytes

type trustedDataModelProjection struct {
	request   recordstorev1.SyncModelsRequest
	providers map[string][]string
}

func (c *ContractValidationCache) projectDataModels(deployment deploymentv2.Deployment, artifacts ArtifactReader) (trustedDataModelProjection, error) {
	if c == nil {
		return projectTrustedDataModels(deployment, artifacts)
	}
	digest := deployment.Digest()
	c.mu.RLock()
	if c.dataModelDigest == digest {
		cached := cloneTrustedDataModelProjection(c.dataModelProjection)
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()
	candidate, err := projectTrustedDataModels(deployment, artifacts)
	if err != nil {
		return trustedDataModelProjection{}, err
	}
	c.mu.Lock()
	if c.dataModelDigest != digest {
		c.dataModelDigest = digest
		c.dataModelProjection = cloneTrustedDataModelProjection(candidate)
	}
	cached := cloneTrustedDataModelProjection(c.dataModelProjection)
	c.mu.Unlock()
	return cached, nil
}

func projectTrustedDataModels(deployment deploymentv2.Deployment, artifacts ArtifactReader) (trustedDataModelProjection, error) {
	projection, err := datamodelinventory.Project(deployment, artifacts)
	if err != nil {
		return trustedDataModelProjection{}, err
	}
	return trustedDataModelProjection{request: projection.Request, providers: projection.Providers}, nil
}

func cloneTrustedDataModelProjection(source trustedDataModelProjection) trustedDataModelProjection {
	cloned := trustedDataModelProjection{request: cloneSyncRequest(source.request), providers: make(map[string][]string, len(source.providers))}
	for unitID, pluginIDs := range source.providers {
		cloned.providers[unitID] = append([]string(nil), pluginIDs...)
	}
	return cloned
}

func cloneSyncRequest(source recordstorev1.SyncModelsRequest) recordstorev1.SyncModelsRequest {
	cloned := recordstorev1.SyncModelsRequest{Generation: source.Generation, InventoryDigest: source.InventoryDigest,
		Models: append([]recordstorev1.SignedModel(nil), source.Models...), Migrations: append([]recordstorev1.SignedMigration(nil), source.Migrations...)}
	if source.SchemaActivation != nil {
		activation := *source.SchemaActivation
		activation.Models = append([]recordstorev1.SchemaMigrationAuthorization(nil), source.SchemaActivation.Models...)
		cloned.SchemaActivation = &activation
	}
	return cloned
}

func injectTrustedDataModels(unit deploymentv2.ServiceUnit, request recordstorev1.SyncModelsRequest, providerPluginIDs []string) (deploymentv2.ServiceUnit, error) {
	pluginIDs := make([]string, 0, len(unit.Plugins))
	for _, plugin := range unit.Plugins {
		pluginIDs = append(pluginIDs, plugin.ID)
	}
	envelope, err := pluginconfig.Parse(unit.Config, pluginIDs)
	if err != nil {
		return deploymentv2.ServiceUnit{}, fmt.Errorf("unit %s 配置信封无效: %w", unit.ID, err)
	}
	for _, pluginID := range providerPluginIDs {
		if envelope.Plugins[pluginID] == nil {
			envelope.Plugins[pluginID] = map[string]any{}
		}
		if _, exists := envelope.Plugins[pluginID][recordstorev1.TrustedInventoryConfigKey]; exists {
			return deploymentv2.ServiceUnit{}, fmt.Errorf("unit %s 用户配置不得声明宿主保留字段 %s", unit.ID, recordstorev1.TrustedInventoryConfigKey)
		}
		envelope.Plugins[pluginID][recordstorev1.TrustedInventoryConfigKey] = request
	}
	unit.Config = envelope.Map()
	return unit, nil
}

func trustedProjectionSize(value recordstorev1.SyncModelsRequest) int {
	raw, _ := json.Marshal(value)
	return len(raw)
}

func sortedProviderIDs(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
