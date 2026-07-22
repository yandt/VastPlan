// Package bootstrapupgrade coordinates repository-stack upgrades without
// giving the repository plugin access to the offline Seed trust boundary.
package bootstrapupgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

type InventoryStore interface {
	Load() (bootstrapinventory.Inventory, error)
	Update(uint64, bootstrapinventory.Inventory) (bootstrapinventory.Inventory, error)
}

type SeedRepository interface {
	Publish(artifacttrust.Attestation, []byte) (pluginv1.Artifact, error)
}

type Candidate struct {
	Artifact     pluginv1.Artifact
	PackageBytes []byte
	Proof        []byte
}

type Controller struct {
	store   InventoryStore
	seed    SeedRepository
	mu      sync.Mutex
	pending map[string]bootstrapinventory.Item
}

func New(store InventoryStore, seed SeedRepository) (*Controller, error) {
	if store == nil || seed == nil {
		return nil, errors.New("Bootstrap Upgrade 必须配置 Inventory Store 与 Seed Repository")
	}
	return &Controller{store: store, seed: seed, pending: map[string]bootstrapinventory.Item{}}, nil
}

// Begin starts one reconcile transaction. Installed items provide restart
// recovery: a candidate already activated before a crash can still advance LKG
// after the new process confirms health.
func (c *Controller) Begin(installed []bootstrapinventory.Item) (bootstrapinventory.Inventory, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inventory, err := c.store.Load()
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	critical, err := criticalPluginItems(inventory)
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	c.pending = map[string]bootstrapinventory.Item{}
	for _, item := range installed {
		current, ok := critical[item.Ref.PluginID]
		if !ok || current == item {
			continue
		}
		order, err := upgradeOrder(current, item)
		if err != nil {
			return bootstrapinventory.Inventory{}, err
		}
		// Commit may durably advance LKG immediately before the Node Agent
		// checkpoints its newer ActualState. After a crash, that checkpoint can
		// still describe the older installed version. It is recovery input, not a
		// downgrade candidate; normal reconcile will install the current LKG.
		if order < 0 {
			continue
		}
		if err := validateUpgrade(current, item); err != nil {
			return bootstrapinventory.Inventory{}, err
		}
		if prior, exists := c.pending[item.Ref.PluginID]; exists && prior != item {
			return bootstrapinventory.Inventory{}, fmt.Errorf("Bootstrap 关键插件 %s 在实际态中存在多个精确制品", item.Ref.PluginID)
		}
		c.pending[item.Ref.PluginID] = item
	}
	return inventory, nil
}

// Prepare mirrors verified critical candidates into Seed and advances only the
// Seed inventory. LKG remains unchanged until Commit is called after health.
func (c *Controller) Prepare(ctx context.Context, candidates []Candidate) (bootstrapinventory.Inventory, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	inventory, err := c.store.Load()
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	critical, err := criticalPluginItems(inventory)
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	seed := append([]bootstrapinventory.Item(nil), inventory.Seed...)
	changed := false
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return bootstrapinventory.Inventory{}, err
		}
		current, ok := critical[candidate.Artifact.PluginID]
		if !ok {
			continue
		}
		item := bootstrapinventory.Item{
			Ref: pluginv1.ArtifactRef{
				PluginID: candidate.Artifact.PluginID,
				Version:  candidate.Artifact.Version,
				Channel:  candidate.Artifact.Channel,
			},
			SHA256: candidate.Artifact.SHA256,
		}
		if current == item {
			continue
		}
		if err := validateUpgrade(current, item); err != nil {
			return bootstrapinventory.Inventory{}, err
		}
		if prior, exists := c.pending[item.Ref.PluginID]; exists && prior != item {
			return bootstrapinventory.Inventory{}, fmt.Errorf("同一 reconcile 包含多个 Bootstrap 候选: %s", item.Ref.PluginID)
		}
		attestation, err := decodeAttestation(candidate.Proof)
		if err != nil {
			return bootstrapinventory.Inventory{}, fmt.Errorf("解析 Bootstrap 候选证明: %w", err)
		}
		if !sameArtifact(attestation.Artifact, candidate.Artifact) {
			return bootstrapinventory.Inventory{}, errors.New("Bootstrap 候选证明与已验证制品不一致")
		}
		published, err := c.seed.Publish(attestation, candidate.PackageBytes)
		if err != nil {
			return bootstrapinventory.Inventory{}, fmt.Errorf("复制 Bootstrap 候选到 Seed: %w", err)
		}
		if !sameArtifact(published, candidate.Artifact) {
			return bootstrapinventory.Inventory{}, errors.New("Seed Repository 返回了不同的 Bootstrap 制品身份")
		}
		item = bootstrapinventory.Item{
			Ref:    pluginv1.ArtifactRef{PluginID: published.PluginID, Version: published.Version, Channel: published.Channel},
			SHA256: published.SHA256,
		}
		c.pending[item.Ref.PluginID] = item
		if !containsItem(seed, item) {
			seed = append(seed, item)
			changed = true
		}
	}
	if !changed {
		return inventory, nil
	}
	next := inventory
	next.Generation++
	next.Seed = seed
	next, err = bootstrapinventory.Normalize(next)
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	return c.store.Update(inventory.Generation, next)
}

// Commit advances the complete LKG snapshot only after the caller has proved
// runtime health and protected the active Assignment. It never removes bytes
// from Seed, so rollback remains possible without rewriting old generations.
func (c *Controller) Commit(ctx context.Context) (bootstrapinventory.Inventory, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	inventory, err := c.store.Load()
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	critical, err := criticalPluginItems(inventory)
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	if len(c.pending) == 0 {
		return inventory, nil
	}
	for pluginID, item := range c.pending {
		current, ok := critical[pluginID]
		if !ok {
			return bootstrapinventory.Inventory{}, fmt.Errorf("Bootstrap LKG 不再包含候选插件 %s", pluginID)
		}
		if current != item {
			if err := validateUpgrade(current, item); err != nil {
				return bootstrapinventory.Inventory{}, err
			}
		}
		if !containsItem(inventory.Seed, item) {
			return bootstrapinventory.Inventory{}, fmt.Errorf("Bootstrap 候选 %s 尚未进入 Seed", pluginID)
		}
	}
	nextLKG := make([]bootstrapinventory.Item, 0, len(inventory.LastKnownGood))
	used := map[string]bool{}
	for _, item := range inventory.LastKnownGood {
		if replacement, ok := c.pending[item.Ref.PluginID]; ok {
			if !used[item.Ref.PluginID] {
				nextLKG = append(nextLKG, replacement)
				used[item.Ref.PluginID] = true
			}
			continue
		}
		nextLKG = append(nextLKG, item)
	}
	if sameItems(inventory.LastKnownGood, nextLKG) {
		c.pending = map[string]bootstrapinventory.Item{}
		return inventory, nil
	}
	next := inventory
	next.Generation++
	next.LastKnownGood = nextLKG
	next, err = bootstrapinventory.Normalize(next)
	if err != nil {
		return bootstrapinventory.Inventory{}, err
	}
	updated, err := c.store.Update(inventory.Generation, next)
	if err == nil {
		c.pending = map[string]bootstrapinventory.Item{}
	}
	return updated, err
}

func criticalPluginItems(inventory bootstrapinventory.Inventory) (map[string]bootstrapinventory.Item, error) {
	values := make(map[string]bootstrapinventory.Item, len(inventory.LastKnownGood))
	for _, item := range inventory.LastKnownGood {
		if _, duplicate := values[item.Ref.PluginID]; duplicate {
			return nil, fmt.Errorf("Bootstrap LKG 对插件 %s 包含多个精确制品", item.Ref.PluginID)
		}
		values[item.Ref.PluginID] = item
	}
	return values, nil
}

func validateUpgrade(current, candidate bootstrapinventory.Item) error {
	order, err := upgradeOrder(current, candidate)
	if err != nil {
		return err
	}
	if order < 0 {
		return fmt.Errorf("Bootstrap 关键插件 %s 不允许自动降级: %s -> %s", current.Ref.PluginID, current.Ref.Version, candidate.Ref.Version)
	}
	return nil
}

func upgradeOrder(current, candidate bootstrapinventory.Item) (int, error) {
	if candidate.Ref.Channel != current.Ref.Channel {
		return 0, fmt.Errorf("Bootstrap 关键插件 %s 不允许自动跨通道升级: %s -> %s", current.Ref.PluginID, current.Ref.Channel, candidate.Ref.Channel)
	}
	currentVersion, err := semver.NewVersion(current.Ref.Version)
	if err != nil {
		return 0, fmt.Errorf("Bootstrap LKG 版本无效: %w", err)
	}
	candidateVersion, err := semver.NewVersion(candidate.Ref.Version)
	if err != nil {
		return 0, fmt.Errorf("Bootstrap 候选版本无效: %w", err)
	}
	return candidateVersion.Compare(currentVersion), nil
}

func containsItem(values []bootstrapinventory.Item, item bootstrapinventory.Item) bool {
	for _, value := range values {
		if value == item {
			return true
		}
	}
	return false
}

func sameItems(left, right []bootstrapinventory.Item) bool {
	left = append([]bootstrapinventory.Item(nil), left...)
	right = append([]bootstrapinventory.Item(nil), right...)
	sort.Slice(left, func(i, j int) bool { return itemKey(left[i]) < itemKey(left[j]) })
	sort.Slice(right, func(i, j int) bool { return itemKey(right[i]) < itemKey(right[j]) })
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func itemKey(item bootstrapinventory.Item) string {
	return item.Ref.PluginID + "@" + item.Ref.Version + "/" + item.Ref.Channel + "\x00" + item.SHA256
}

func decodeAttestation(raw []byte) (artifacttrust.Attestation, error) {
	var value artifacttrust.Attestation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return value, errors.New("制品证明只能包含一个 JSON 值")
	}
	return value, nil
}

func sameArtifact(left, right pluginv1.Artifact) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return bytes.Equal(a, b)
}
