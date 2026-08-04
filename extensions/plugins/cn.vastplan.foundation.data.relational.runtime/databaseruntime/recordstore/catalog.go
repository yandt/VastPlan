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

	datamodelv1 "cdsoft.com.cn/VastPlan/contracts/schemas/datamodel/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

type ModelEntry struct {
	OwnerPluginID string
	Ref           recordstorev1.ModelRef
	Model         datamodelv1.Model
}

type Catalog struct {
	mu         sync.RWMutex
	generation uint64
	digest     [32]byte
	models     map[string]ModelEntry
}

func NewCatalog() *Catalog { return &Catalog{models: map[string]ModelEntry{}} }

func (c *Catalog) Replace(request recordstorev1.SyncModelsRequest) (recordstorev1.SyncModelsResult, error) {
	if c == nil || request.Generation == 0 || len(request.Models) > 512 {
		return recordstorev1.SyncModelsResult{}, errors.New("Record Store 模型目录请求无效")
	}
	models := append([]recordstorev1.SignedModel(nil), request.Models...)
	sort.Slice(models, func(i, j int) bool { return models[i].Model.ID < models[j].Model.ID })
	next := make(map[string]ModelEntry, len(models))
	canonical := make([]byte, 0, len(request.Models)*96)
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
		if model.ID != signed.Model.ID || model.SchemaVersion != signed.Model.SchemaVersion || signed.OwnerPluginID == "" {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("DataModel %s 身份不匹配", signed.Model.ID)
		}
		if _, duplicate := next[model.ID]; duplicate {
			return recordstorev1.SyncModelsResult{}, fmt.Errorf("DataModel %s 重复", model.ID)
		}
		next[model.ID] = ModelEntry{OwnerPluginID: signed.OwnerPluginID, Ref: signed.Model, Model: model}
		canonical = append(canonical, []byte(signed.OwnerPluginID+"\x00"+model.ID+"\x00"+digest+"\n")...)
	}
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
		return recordstorev1.SyncModelsResult{Generation: c.generation, Models: len(c.models)}, nil
	}
	c.generation, c.digest, c.models = request.Generation, nextDigest, next
	return recordstorev1.SyncModelsResult{Generation: c.generation, Models: len(c.models)}, nil
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

func EncodeSignedModel(owner string, modelRef recordstorev1.ModelRef, document []byte) recordstorev1.SignedModel {
	return recordstorev1.SignedModel{OwnerPluginID: owner, Model: modelRef, DocumentBase64: base64.StdEncoding.EncodeToString(document)}
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
