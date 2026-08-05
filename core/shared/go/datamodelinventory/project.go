// Package datamodelinventory projects signed DataModel documents from exact
// deployment artifacts. It is shared by preview publication and scheduling so
// both paths bind the same inventory digest and owner identities.
package datamodelinventory

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	datamigrationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamigration/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
)

const (
	MaxDocumentBytes  = 256 << 10
	MaxInventoryBytes = 1 << 20
)

type ArtifactReader interface {
	Read(pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error)
}

type Projection struct {
	Request   recordstorev1.SyncModelsRequest
	Providers map[string][]string
	Catalog   deploymentpublication.DataModelCatalog
}

func Project(deployment deploymentv2.Deployment, artifacts ArtifactReader) (Projection, error) {
	projection := Projection{Providers: map[string][]string{}}
	if artifacts == nil {
		return projection, nil
	}
	seenArtifacts := map[pluginv1.ArtifactRef]struct{}{}
	modelOwners := map[string]string{}
	migrationOwners := map[string]string{}
	for _, unit := range deployment.Units {
		if !unit.Enabled {
			continue
		}
		pluginIDs := make([]string, 0, len(unit.Plugins))
		for _, locked := range unit.Plugins {
			pluginIDs = append(pluginIDs, locked.ID)
		}
		envelope, err := pluginconfig.Parse(unit.Config, pluginIDs)
		if err != nil {
			return projection, fmt.Errorf("投影 DataModel 时解析 unit %s 配置: %w", unit.ID, err)
		}
		for _, locked := range unit.Plugins {
			ref := pluginv1.ArtifactRef{PluginID: locked.ID, Version: locked.Version, Channel: normalizedChannel(locked.Channel)}
			artifact, packageBytes, err := artifacts.Read(ref)
			if err != nil {
				return projection, fmt.Errorf("投影 DataModel 时读取制品 %s@%s: %w", ref.PluginID, ref.Version, err)
			}
			manifest, err := pluginv1.ParseManifest(artifact.Manifest)
			if err != nil || artifact.PluginID != ref.PluginID || artifact.Version != ref.Version || artifact.SHA256 != locked.SHA256 || manifest.ID != ref.PluginID || manifest.Version != ref.Version {
				return projection, fmt.Errorf("投影 DataModel 时制品身份不一致: %s@%s", ref.PluginID, ref.Version)
			}
			if providesRecordStore(manifest) {
				projection.Providers[unit.ID] = append(projection.Providers[unit.ID], manifest.ID)
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
				signed, model, err := materializeModel(manifest.ID, artifact.SHA256, packageBytes, declared)
				if err != nil {
					return projection, err
				}
				if owner, duplicate := modelOwners[signed.Model.ID]; duplicate {
					return projection, fmt.Errorf("DataModel %s 由多个插件声明: %s, %s", signed.Model.ID, owner, manifest.ID)
				}
				modelOwners[signed.Model.ID] = manifest.ID
				projection.Request.Models = append(projection.Request.Models, signed)
				storage, err := resolveStorageTarget(model, envelope.Plugins[manifest.ID])
				if err != nil {
					return projection, fmt.Errorf("插件 %s DataModel %s: %w", manifest.ID, model.ID, err)
				}
				projection.Catalog.Models = append(projection.Catalog.Models, deploymentpublication.DataModelDescriptor{
					OwnerPluginID: manifest.ID, ArtifactSHA256: artifact.SHA256, Ref: signed.Model, Model: model, Storage: storage,
				})
			}
			migrations, err := pluginv1.ManifestDataMigrations(manifest)
			if err != nil {
				return projection, fmt.Errorf("读取插件 %s DataMigration 引用: %w", manifest.ID, err)
			}
			for _, declared := range migrations {
				signed, err := materializeMigration(manifest.ID, artifact.SHA256, packageBytes, declared)
				if err != nil {
					return projection, err
				}
				if owner, duplicate := migrationOwners[signed.Migration.ID]; duplicate {
					return projection, fmt.Errorf("DataMigration %s 由多个插件声明: %s, %s", signed.Migration.ID, owner, manifest.ID)
				}
				migrationOwners[signed.Migration.ID] = manifest.ID
				projection.Request.Migrations = append(projection.Request.Migrations, signed)
				projection.Catalog.Migrations = append(projection.Catalog.Migrations, deploymentpublication.DataMigrationDescriptor{
					OwnerPluginID: manifest.ID, ArtifactSHA256: artifact.SHA256, Ref: signed.Migration,
				})
			}
		}
	}
	sortProjection(&projection)
	projection.Request.Generation = deployment.Revision
	projection.Request.InventoryDigest = deployment.Digest()
	projection.Request.SchemaActivation = deployment.Resolution.SchemaActivation
	projection.Catalog.Digest = catalogDigest(projection.Catalog)
	raw, err := json.Marshal(projection.Request)
	if err != nil || len(raw) > MaxInventoryBytes {
		return projection, fmt.Errorf("可信 DataModel Inventory 超过 %d 字节上限", MaxInventoryBytes)
	}
	return projection, nil
}

func resolveStorageTarget(model datamodelv1.Model, configuration map[string]any) (recordstorev1.StorageTarget, error) {
	if model.Storage.Kind == "platform-control" {
		return recordstorev1.StorageTarget{}, nil
	}
	bindings, ok := configuration[recordstorev1.StorageBindingsConfigKey]
	if !ok {
		return recordstorev1.StorageTarget{}, fmt.Errorf("connection-ref 模型缺少 %s 配置", recordstorev1.StorageBindingsConfigKey)
	}
	rawBindings, err := json.Marshal(bindings)
	if err != nil {
		return recordstorev1.StorageTarget{}, err
	}
	var values map[string]recordstorev1.StorageTarget
	if err := json.Unmarshal(rawBindings, &values); err != nil {
		return recordstorev1.StorageTarget{}, errors.New("Record Store 存储绑定格式无效")
	}
	target, ok := values[model.ID]
	if !ok || target.Connection == nil || databasev1.ValidateConnectionRef(*target.Connection) != nil {
		return recordstorev1.StorageTarget{}, errors.New("connection-ref 模型缺少精确活动连接 revision")
	}
	return target, nil
}

func materializeModel(owner, artifactDigest string, packageBytes []byte, declared pluginv1.DataModelReference) (recordstorev1.SignedModel, datamodelv1.Model, error) {
	if declared.ContractVersion != datamodelv1.ContractVersion {
		return recordstorev1.SignedModel{}, datamodelv1.Model{}, fmt.Errorf("插件 %s DataModel %s 契约版本不受支持: %s", owner, declared.ID, declared.ContractVersion)
	}
	document, err := artifacttrust.ReadPackageFile(packageBytes, declared.Path, MaxDocumentBytes)
	if err != nil {
		return recordstorev1.SignedModel{}, datamodelv1.Model{}, fmt.Errorf("读取插件 %s DataModel %s: %w", owner, declared.ID, err)
	}
	digest := sha256.Sum256(document)
	encoded := hex.EncodeToString(digest[:])
	model, err := datamodelv1.Parse(document)
	if err != nil || model.ID != declared.ID || encoded != declared.SHA256 {
		return recordstorev1.SignedModel{}, datamodelv1.Model{}, fmt.Errorf("插件 %s DataModel %s 内容与签名引用不一致", owner, declared.ID)
	}
	signed := recordstorev1.SignedModel{OwnerPluginID: owner, ArtifactSHA256: artifactDigest,
		Model:          recordstorev1.ModelRef{ID: model.ID, SchemaVersion: model.SchemaVersion, SHA256: encoded},
		DocumentBase64: base64.StdEncoding.EncodeToString(document)}
	return signed, model, nil
}

func materializeMigration(owner, artifactDigest string, packageBytes []byte, declared pluginv1.DataMigrationReference) (recordstorev1.SignedMigration, error) {
	if declared.ContractVersion != datamigrationv1.ContractVersion {
		return recordstorev1.SignedMigration{}, fmt.Errorf("插件 %s DataMigration %s 契约版本不受支持: %s", owner, declared.ID, declared.ContractVersion)
	}
	document, err := artifacttrust.ReadPackageFile(packageBytes, declared.Path, MaxDocumentBytes)
	if err != nil {
		return recordstorev1.SignedMigration{}, fmt.Errorf("读取插件 %s DataMigration %s: %w", owner, declared.ID, err)
	}
	digest := sha256.Sum256(document)
	encoded := hex.EncodeToString(digest[:])
	migration, err := datamigrationv1.Parse(document)
	if err != nil || migration.ID != declared.ID || migration.ModelID != declared.ModelID || migration.From.SchemaVersion != declared.FromVersion || migration.To.SchemaVersion != declared.ToVersion || encoded != declared.SHA256 {
		return recordstorev1.SignedMigration{}, fmt.Errorf("插件 %s DataMigration %s 内容与签名引用不一致", owner, declared.ID)
	}
	return recordstorev1.SignedMigration{OwnerPluginID: owner, ArtifactSHA256: artifactDigest,
		Migration:      recordstorev1.MigrationRef{ID: migration.ID, ModelID: migration.ModelID, FromVersion: migration.From.SchemaVersion, ToVersion: migration.To.SchemaVersion, SHA256: encoded},
		DocumentBase64: base64.StdEncoding.EncodeToString(document)}, nil
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

func sortProjection(projection *Projection) {
	sort.Slice(projection.Request.Models, func(i, j int) bool {
		return projection.Request.Models[i].Model.ID < projection.Request.Models[j].Model.ID
	})
	sort.Slice(projection.Request.Migrations, func(i, j int) bool {
		return projection.Request.Migrations[i].Migration.ID < projection.Request.Migrations[j].Migration.ID
	})
	sort.Slice(projection.Catalog.Models, func(i, j int) bool { return projection.Catalog.Models[i].Ref.ID < projection.Catalog.Models[j].Ref.ID })
	sort.Slice(projection.Catalog.Migrations, func(i, j int) bool {
		return projection.Catalog.Migrations[i].Ref.ID < projection.Catalog.Migrations[j].Ref.ID
	})
	for unitID := range projection.Providers {
		sort.Strings(projection.Providers[unitID])
	}
}

func catalogDigest(catalog deploymentpublication.DataModelCatalog) string {
	catalog.Digest = ""
	raw, _ := json.Marshal(catalog)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func normalizedChannel(channel string) string {
	if channel == "" {
		return "stable"
	}
	return channel
}
