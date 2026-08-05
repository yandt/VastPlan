// Package recordstore implements the declarative Record Store module inside
// the Database Runtime plugin. It is a module, not a separately deployed plugin.
package recordstore

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	datamigrationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamigration/v1"
	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

type ModelEntry struct {
	OwnerPluginID  string
	ArtifactSHA256 string
	Ref            recordstorev1.ModelRef
	Model          datamodelv1.Model
}

type MigrationEntry struct {
	OwnerPluginID  string
	ArtifactSHA256 string
	Ref            recordstorev1.MigrationRef
	Migration      datamigrationv1.Migration
}

type Catalog struct {
	mu         sync.RWMutex
	generation uint64
	digest     [32]byte
	models     map[string]ModelEntry
	migrations map[string]MigrationEntry
}

func NewCatalog() *Catalog {
	return &Catalog{models: map[string]ModelEntry{}, migrations: map[string]MigrationEntry{}}
}

// PlatformModels returns a stable copy of models explicitly bound to the
// reserved Platform Control store. Callers cannot mutate the live catalog.
func (c *Catalog) PlatformModels() []ModelEntry {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	result := make([]ModelEntry, 0, len(c.models))
	for _, entry := range c.models {
		if entry.Model.Storage.Kind == "platform-control" {
			result = append(result, entry)
		}
	}
	c.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Model.ID < result[j].Model.ID })
	return result
}

func (c *Catalog) Replace(request recordstorev1.SyncModelsRequest) (recordstorev1.SyncModelsResult, error) {
	if c == nil || request.Generation == 0 || !commonv1.IsSHA256(request.InventoryDigest) || len(request.Models) > 512 || len(request.Migrations) > 1024 {
		return recordstorev1.SyncModelsResult{}, errors.New("Record Store 模型目录请求无效")
	}
	models := append([]recordstorev1.SignedModel(nil), request.Models...)
	sort.Slice(models, func(i, j int) bool { return models[i].Model.ID < models[j].Model.ID })
	next := make(map[string]ModelEntry, len(models))
	canonical := make([]byte, 0, (len(request.Models)+len(request.Migrations))*128)
	for _, signed := range models {
		raw, err := base64.StdEncoding.DecodeString(signed.DocumentBase64)
		if err != nil {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("解码 DataModel %s: %w", signed.Model.ID, err)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(raw))
		if digest != signed.Model.SHA256 {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("DataModel %s 摘要不匹配", signed.Model.ID)
		}
		model, err := datamodelv1.Parse(raw)
		if err != nil {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("解析 DataModel %s: %w", signed.Model.ID, err)
		}
		if model.ID != signed.Model.ID || model.SchemaVersion != signed.Model.SchemaVersion || signed.OwnerPluginID == "" || !commonv1.IsSHA256(signed.ArtifactSHA256) {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("DataModel %s 身份不匹配", signed.Model.ID)
		}
		if _, duplicate := next[model.ID]; duplicate {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("DataModel %s 重复", model.ID)
		}
		next[model.ID] = ModelEntry{OwnerPluginID: signed.OwnerPluginID, ArtifactSHA256: signed.ArtifactSHA256, Ref: signed.Model, Model: model}
		canonical = append(canonical, []byte(signed.OwnerPluginID+"\x00"+signed.ArtifactSHA256+"\x00"+model.ID+"\x00"+digest+"\n")...)
	}
	migrations := append([]recordstorev1.SignedMigration(nil), request.Migrations...)
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Migration.ID < migrations[j].Migration.ID })
	nextMigrations := make(map[string]MigrationEntry, len(migrations))
	edges := map[string]struct{}{}
	for _, signed := range migrations {
		raw, err := base64.StdEncoding.DecodeString(signed.DocumentBase64)
		if err != nil {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("解码 DataMigration %s: %w", signed.Migration.ID, err)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(raw))
		if digest != signed.Migration.SHA256 {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("DataMigration %s 摘要不匹配", signed.Migration.ID)
		}
		migration, err := datamigrationv1.Parse(raw)
		if err != nil {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("解析 DataMigration %s: %w", signed.Migration.ID, err)
		}
		if migration.ID != signed.Migration.ID || migration.ModelID != signed.Migration.ModelID || migration.From.SchemaVersion != signed.Migration.FromVersion || migration.To.SchemaVersion != signed.Migration.ToVersion || signed.OwnerPluginID == "" || !commonv1.IsSHA256(signed.ArtifactSHA256) {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("DataMigration %s 身份不匹配", signed.Migration.ID)
		}
		model, ok := next[migration.ModelID]
		if !ok || model.OwnerPluginID != signed.OwnerPluginID || model.ArtifactSHA256 != signed.ArtifactSHA256 || migration.To.SchemaVersion > model.Model.SchemaVersion || migration.To.SchemaVersion == model.Model.SchemaVersion && migration.To.SHA256 != model.Ref.SHA256 {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("DataMigration %s 未绑定同制品 DataModel", signed.Migration.ID)
		}
		if _, duplicate := nextMigrations[migration.ID]; duplicate {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("DataMigration %s 重复", migration.ID)
		}
		edge := fmt.Sprintf("%s:%d:%d", migration.ModelID, migration.From.SchemaVersion, migration.To.SchemaVersion)
		if _, duplicate := edges[edge]; duplicate {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("DataMigration 版本边重复: %s", edge)
		}
		edges[edge] = struct{}{}
		nextMigrations[migration.ID] = MigrationEntry{OwnerPluginID: signed.OwnerPluginID, ArtifactSHA256: signed.ArtifactSHA256, Ref: signed.Migration, Migration: migration}
		canonical = append(canonical, []byte("migration\x00"+signed.OwnerPluginID+"\x00"+signed.ArtifactSHA256+"\x00"+migration.ID+"\x00"+digest+"\n")...)
	}
	canonical = append([]byte("inventory\x00"+request.InventoryDigest+"\n"), canonical...)
	nextDigest := sha256.Sum256(canonical)
	c.mu.Lock()
	defer c.mu.Unlock()
	if request.Generation < c.generation {
		return recordstorev1.SyncModelsResult{}, errors.New("Record Store 模型目录 generation 回退")
	}
	if request.Generation == c.generation {
		if nextDigest != c.digest {
			return recordstorev1.SyncModelsResult{}, errors.New("Record Store 同 generation 内容漂移")
		}
		return recordstorev1.SyncModelsResult{Generation: c.generation, Models: len(c.models), Migrations: len(c.migrations)}, nil
	}
	c.generation, c.digest, c.models, c.migrations = request.Generation, nextDigest, next, nextMigrations
	return recordstorev1.SyncModelsResult{Generation: c.generation, Models: len(c.models), Migrations: len(c.migrations)}, nil
}

func (c *Catalog) ResolveMigration(model ModelEntry, state SchemaState, migrationID string) (MigrationEntry, error) {
	if c == nil || migrationID == "" {
		return MigrationEntry{}, ErrMigrationNeeded
	}
	c.mu.RLock()
	entry, ok := c.migrations[migrationID]
	c.mu.RUnlock()
	if !ok || entry.OwnerPluginID != model.OwnerPluginID || entry.ArtifactSHA256 != model.ArtifactSHA256 || entry.Migration.ModelID != model.Model.ID ||
		entry.Migration.From.SchemaVersion != state.Version || entry.Migration.From.SHA256 != state.SHA256 ||
		entry.Migration.To.SchemaVersion != model.Ref.SchemaVersion || entry.Migration.To.SHA256 != model.Ref.SHA256 {
		return MigrationEntry{}, ErrMigrationNeeded
	}
	return entry, nil
}

func (c *Catalog) Resolve(ref recordstorev1.ModelRef) (ModelEntry, error) {
	if c == nil {
		return ModelEntry{}, ErrModelNotFound
	}
	c.mu.RLock()
	entry, ok := c.models[ref.ID]
	c.mu.RUnlock()
	if !ok {
		return ModelEntry{}, fmt.Errorf("%w: %s", ErrModelNotFound, ref.ID)
	}
	if entry.Ref != ref {
		return ModelEntry{}, fmt.Errorf("%w: %s", ErrModelMismatch, ref.ID)
	}
	return entry, nil
}

func EncodeSignedModel(owner, artifactSHA256 string, modelRef recordstorev1.ModelRef, document []byte) recordstorev1.SignedModel {
	return recordstorev1.SignedModel{OwnerPluginID: owner, ArtifactSHA256: artifactSHA256, Model: modelRef, DocumentBase64: base64.StdEncoding.EncodeToString(document)}
}

func EncodeSignedMigration(owner, artifactSHA256 string, ref recordstorev1.MigrationRef, document []byte) recordstorev1.SignedMigration {
	return recordstorev1.SignedMigration{OwnerPluginID: owner, ArtifactSHA256: artifactSHA256, Migration: ref, DocumentBase64: base64.StdEncoding.EncodeToString(document)}
}

func ModelRef(document []byte) (recordstorev1.ModelRef, error) {
	model, err := datamodelv1.Parse(document)
	if err != nil {
		return recordstorev1.ModelRef{}, err
	}
	return recordstorev1.ModelRef{ID: model.ID, SchemaVersion: model.SchemaVersion, SHA256: fmt.Sprintf("%x", sha256.Sum256(document))}, nil
}

// MarshalModel is used only by deterministic tests and trusted catalog tools.
func MarshalModel(model datamodelv1.Model) ([]byte, error) { return json.Marshal(model) }
