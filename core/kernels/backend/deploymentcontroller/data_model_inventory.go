package deploymentcontroller

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	datamigrationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamigration/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
)

const (
	maxDataModelDocumentBytes  = 256 << 10
	maxDataModelInventoryBytes = 1 << 20
)

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

func cloneTrustedDataModelProjection(source trustedDataModelProjection) trustedDataModelProjection {
	cloned := trustedDataModelProjection{
		request: recordstorev1.SyncModelsRequest{
			Generation: source.request.Generation, InventoryDigest: source.request.InventoryDigest,
			Models:     append([]recordstorev1.SignedModel(nil), source.request.Models...),
			Migrations: append([]recordstorev1.SignedMigration(nil), source.request.Migrations...),
		},
		providers: make(map[string][]string, len(source.providers)),
	}
	for unitID, pluginIDs := range source.providers {
		cloned.providers[unitID] = append([]string(nil), pluginIDs...)
	}
	return cloned
}

// projectTrustedDataModels derives the Runtime catalog only from exact
// artifacts already locked by the Deployment. The resulting request is not a
// user setting: injectTrustedDataModels adds it after ordinary configuration
// validation and only to plugins that provide the Record Store capability.
func projectTrustedDataModels(deployment deploymentv2.Deployment, artifacts ArtifactReader) (trustedDataModelProjection, error) {
	projection := trustedDataModelProjection{providers: map[string][]string{}}
	if artifacts == nil {
		return projection, nil
	}
	seenArtifacts := map[pluginv1.ArtifactRef]struct{}{}
	modelOwners := map[string]string{}
	migrationIDs := map[string]string{}
	for _, unit := range deployment.Units {
		if !unit.Enabled {
			continue
		}
		for _, locked := range unit.Plugins {
			ref := pluginv1.ArtifactRef{PluginID: locked.ID, Version: locked.Version, Channel: normalizedChannel(locked)}
			artifact, packageBytes, err := artifacts.Read(ref)
			if err != nil {
				return projection, fmt.Errorf("投影 DataModel 时读取制品 %s@%s: %w", ref.PluginID, ref.Version, err)
			}
			manifest, err := pluginv1.ParseManifest(artifact.Manifest)
			if err != nil || artifact.PluginID != ref.PluginID || artifact.Version != ref.Version || artifact.SHA256 != locked.SHA256 || manifest.ID != ref.PluginID || manifest.Version != ref.Version {
				return projection, fmt.Errorf("投影 DataModel 时制品身份不一致: %s@%s", ref.PluginID, ref.Version)
			}
			if providesRecordStore(manifest) {
				projection.providers[unit.ID] = append(projection.providers[unit.ID], manifest.ID)
			}
			if _, seen := seenArtifacts[ref]; seen {
				continue
			}
			seenArtifacts[ref] = struct{}{}
			models, err := pluginv1.ManifestDataModels(manifest)
			if err != nil {
				return projection, fmt.Errorf("读取插件 %s DataModel 引用: %w", manifest.ID, err)
			}
			for _, declared := range models {
				signed, err := materializeSignedModel(manifest.ID, artifact.SHA256, packageBytes, declared)
				if err != nil {
					return projection, err
				}
				if owner, duplicate := modelOwners[signed.Model.ID]; duplicate {
					return projection, fmt.Errorf("DataModel %s 由多个插件声明: %s, %s", signed.Model.ID, owner, manifest.ID)
				}
				modelOwners[signed.Model.ID] = manifest.ID
				projection.request.Models = append(projection.request.Models, signed)
			}
			migrations, err := pluginv1.ManifestDataMigrations(manifest)
			if err != nil {
				return projection, fmt.Errorf("读取插件 %s DataMigration 引用: %w", manifest.ID, err)
			}
			for _, declared := range migrations {
				signed, err := materializeSignedMigration(manifest.ID, artifact.SHA256, packageBytes, declared)
				if err != nil {
					return projection, err
				}
				if owner, duplicate := migrationIDs[signed.Migration.ID]; duplicate {
					return projection, fmt.Errorf("DataMigration %s 由多个插件声明: %s, %s", signed.Migration.ID, owner, manifest.ID)
				}
				migrationIDs[signed.Migration.ID] = manifest.ID
				projection.request.Migrations = append(projection.request.Migrations, signed)
			}
		}
	}
	sort.Slice(projection.request.Models, func(i, j int) bool {
		left, right := projection.request.Models[i], projection.request.Models[j]
		return left.Model.ID+"\x00"+left.OwnerPluginID < right.Model.ID+"\x00"+right.OwnerPluginID
	})
	sort.Slice(projection.request.Migrations, func(i, j int) bool {
		left, right := projection.request.Migrations[i], projection.request.Migrations[j]
		return left.Migration.ID+"\x00"+left.OwnerPluginID < right.Migration.ID+"\x00"+right.OwnerPluginID
	})
	projection.request.Generation = deployment.Revision
	projection.request.InventoryDigest = deployment.Digest()
	raw, err := json.Marshal(projection.request)
	if err != nil || len(raw) > maxDataModelInventoryBytes {
		return projection, fmt.Errorf("可信 DataModel Inventory 超过 %d 字节上限", maxDataModelInventoryBytes)
	}
	for unitID := range projection.providers {
		sort.Strings(projection.providers[unitID])
	}
	return projection, nil
}

func providesRecordStore(manifest pluginv1.Manifest) bool {
	if manifest.Runtime == nil {
		return false
	}
	for _, provided := range manifest.Runtime.Provides {
		if provided.Capability == recordstorev1.Capability && provided.ContractVersion == recordstorev1.ContractVersion {
			return true
		}
	}
	return false
}

func materializeSignedModel(owner, artifactDigest string, packageBytes []byte, declared pluginv1.DataModelReference) (recordstorev1.SignedModel, error) {
	if declared.ContractVersion != datamodelv1.ContractVersion {
		return recordstorev1.SignedModel{}, fmt.Errorf("插件 %s DataModel %s 契约版本不受支持: %s", owner, declared.ID, declared.ContractVersion)
	}
	document, err := artifacttrust.ReadPackageFile(packageBytes, declared.Path, maxDataModelDocumentBytes)
	if err != nil {
		return recordstorev1.SignedModel{}, fmt.Errorf("读取插件 %s DataModel %s: %w", owner, declared.ID, err)
	}
	digest := sha256.Sum256(document)
	encodedDigest := hex.EncodeToString(digest[:])
	model, err := datamodelv1.Parse(document)
	if err != nil || model.ID != declared.ID || encodedDigest != declared.SHA256 {
		return recordstorev1.SignedModel{}, fmt.Errorf("插件 %s DataModel %s 内容与签名引用不一致", owner, declared.ID)
	}
	return recordstorev1.SignedModel{
		OwnerPluginID: owner, ArtifactSHA256: artifactDigest,
		Model:          recordstorev1.ModelRef{ID: model.ID, SchemaVersion: model.SchemaVersion, SHA256: encodedDigest},
		DocumentBase64: base64.StdEncoding.EncodeToString(document),
	}, nil
}

func materializeSignedMigration(owner, artifactDigest string, packageBytes []byte, declared pluginv1.DataMigrationReference) (recordstorev1.SignedMigration, error) {
	if declared.ContractVersion != datamigrationv1.ContractVersion {
		return recordstorev1.SignedMigration{}, fmt.Errorf("插件 %s DataMigration %s 契约版本不受支持: %s", owner, declared.ID, declared.ContractVersion)
	}
	document, err := artifacttrust.ReadPackageFile(packageBytes, declared.Path, maxDataModelDocumentBytes)
	if err != nil {
		return recordstorev1.SignedMigration{}, fmt.Errorf("读取插件 %s DataMigration %s: %w", owner, declared.ID, err)
	}
	digest := sha256.Sum256(document)
	encodedDigest := hex.EncodeToString(digest[:])
	migration, err := datamigrationv1.Parse(document)
	if err != nil || migration.ID != declared.ID || migration.ModelID != declared.ModelID || migration.From.SchemaVersion != declared.FromVersion || migration.To.SchemaVersion != declared.ToVersion || encodedDigest != declared.SHA256 {
		return recordstorev1.SignedMigration{}, fmt.Errorf("插件 %s DataMigration %s 内容与签名引用不一致", owner, declared.ID)
	}
	return recordstorev1.SignedMigration{
		OwnerPluginID: owner, ArtifactSHA256: artifactDigest,
		Migration:      recordstorev1.MigrationRef{ID: migration.ID, ModelID: migration.ModelID, FromVersion: migration.From.SchemaVersion, ToVersion: migration.To.SchemaVersion, SHA256: encodedDigest},
		DocumentBase64: base64.StdEncoding.EncodeToString(document),
	}, nil
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
