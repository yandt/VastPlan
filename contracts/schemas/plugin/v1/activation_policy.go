package pluginv1

import (
	"errors"
	"fmt"
	"sort"
)

const (
	PluginTargetBackend  = "backend"
	PluginTargetFrontend = "frontend"
	PluginTargetRunner   = "runner"
	PluginTargetMobile   = "mobile"
)

// ActivationPolicy is selected once at a composition root. Downstream
// planners receive only its immutable decision, never environment flags.
type ActivationPolicy interface {
	ID() string
	Target() string
	Select(PluginInventorySnapshot, ContributionIndexSnapshot) (ActivationSelection, error)
}

type ActivationSelection struct {
	SchemaVersion      int                      `json:"schemaVersion"`
	PolicyID           string                   `json:"policyId"`
	Target             string                   `json:"target"`
	Generation         uint64                   `json:"generation"`
	InventoryDigest    string                   `json:"inventoryDigest"`
	ContributionDigest string                   `json:"contributionDigest"`
	Artifacts          []PluginArtifactIdentity `json:"artifacts"`
	Digest             string                   `json:"digest"`
}

// ExplicitActivationPolicy models production Profile/Application choices.
// Every root is exact; dependencies may be derived only when the Inventory has
// one unambiguous compatible candidate.
type ExplicitActivationPolicy struct {
	PolicyID string
	Kernel   string
	Roots    []ArtifactRef
}

func (p ExplicitActivationPolicy) ID() string     { return p.PolicyID }
func (p ExplicitActivationPolicy) Target() string { return p.Kernel }
func (p ExplicitActivationPolicy) Select(inventory PluginInventorySnapshot, index ContributionIndexSnapshot) (ActivationSelection, error) {
	if p.PolicyID == "" || !validPluginTarget(p.Kernel) || len(p.Roots) == 0 {
		return ActivationSelection{}, errors.New("显式 Activation Policy 无效")
	}
	return selectActivation(p.PolicyID, p.Kernel, p.Roots, false, inventory, index)
}

// DevelopmentActivationPolicy is the only policy that may select discovered
// workspace candidates automatically. Ambiguous same-ID candidates fail
// closed instead of silently choosing by scan order or timestamp.
type DevelopmentActivationPolicy struct {
	PolicyID string
	Kernel   string
}

func (p DevelopmentActivationPolicy) ID() string     { return p.PolicyID }
func (p DevelopmentActivationPolicy) Target() string { return p.Kernel }
func (p DevelopmentActivationPolicy) Select(inventory PluginInventorySnapshot, index ContributionIndexSnapshot) (ActivationSelection, error) {
	if p.PolicyID == "" || !validPluginTarget(p.Kernel) {
		return ActivationSelection{}, errors.New("开发 Activation Policy 无效")
	}
	return selectActivation(p.PolicyID, p.Kernel, nil, true, inventory, index)
}

func selectActivation(policyID, target string, roots []ArtifactRef, workspace bool, inventory PluginInventorySnapshot, index ContributionIndexSnapshot) (ActivationSelection, error) {
	if err := ValidatePluginInventory(inventory); err != nil {
		return ActivationSelection{}, err
	}
	if err := ValidateContributionIndex(index); err != nil || index.InventoryDigest != inventory.Digest || index.Generation != inventory.Generation {
		return ActivationSelection{}, errors.New("Activation Policy 的 Inventory 与 Contribution Index 不一致")
	}
	items := make(map[string][]PluginInventoryItem)
	for _, item := range inventory.Plugins {
		if inventorySupportsTarget(item, target) {
			items[item.Artifact.Ref.PluginID] = append(items[item.Artifact.Ref.PluginID], item)
		}
	}
	selected := map[string]PluginInventoryItem{}
	queue := append([]ArtifactRef(nil), roots...)
	if workspace {
		ids := make([]string, 0, len(items))
		for id := range items {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			candidates := workspaceCandidates(items[id])
			if len(candidates) > 1 {
				return ActivationSelection{}, fmt.Errorf("开发 Activation Policy 的 workspace 候选未消歧: %s", id)
			}
			if len(candidates) == 1 {
				queue = append(queue, candidates[0].Artifact.Ref)
			}
		}
	}
	for len(queue) > 0 {
		ref := normalizeArtifactRef(queue[0])
		queue = queue[1:]
		item, err := exactInventoryItem(items[ref.PluginID], ref)
		if err != nil {
			return ActivationSelection{}, err
		}
		if existing, duplicate := selected[ref.PluginID]; duplicate {
			if artifactIdentityKey(existing.Artifact) != artifactIdentityKey(item.Artifact) {
				return ActivationSelection{}, fmt.Errorf("Activation Policy 为同一插件选择了多个版本: %s", ref.PluginID)
			}
			continue
		}
		selected[ref.PluginID] = item
		for _, dependency := range item.Dependencies {
			candidate, err := compatibleDependency(items[dependency.PluginID], dependency)
			if err != nil {
				return ActivationSelection{}, fmt.Errorf("插件 %s 的依赖无效: %w", ref.PluginID, err)
			}
			queue = append(queue, candidate.Artifact.Ref)
		}
	}
	artifacts := make([]PluginArtifactIdentity, 0, len(selected))
	for _, item := range selected {
		artifacts = append(artifacts, item.Artifact)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifactIdentityKey(artifacts[i]) < artifactIdentityKey(artifacts[j]) })
	selection := ActivationSelection{SchemaVersion: PluginInventorySchemaVersion, PolicyID: policyID, Target: target, Generation: inventory.Generation, InventoryDigest: inventory.Digest, ContributionDigest: index.Digest, Artifacts: artifacts}
	digest, err := activationSelectionDigest(selection)
	if err != nil {
		return ActivationSelection{}, err
	}
	selection.Digest = digest
	return selection, nil
}

func inventorySupportsTarget(item PluginInventoryItem, target string) bool {
	for _, candidate := range item.Targets {
		if candidate.Surface == target {
			return true
		}
	}
	return false
}

func workspaceCandidates(items []PluginInventoryItem) []PluginInventoryItem {
	values := []PluginInventoryItem{}
	for _, item := range items {
		if item.Artifact.Ref.Channel == "workspace" {
			values = append(values, item)
		}
	}
	return values
}

func exactInventoryItem(items []PluginInventoryItem, ref ArtifactRef) (PluginInventoryItem, error) {
	for _, item := range items {
		if item.Artifact.Ref == ref {
			return item, nil
		}
	}
	return PluginInventoryItem{}, fmt.Errorf("Activation Policy 引用了未发现的精确制品: %s@%s/%s", ref.PluginID, ref.Version, ref.Channel)
}

func compatibleDependency(items []PluginInventoryItem, dependency PluginDependency) (PluginInventoryItem, error) {
	compatible := []PluginInventoryItem{}
	for _, item := range items {
		if checkVersionRange(dependency.Constraint, item.Artifact.Ref.Version) == nil {
			compatible = append(compatible, item)
		}
	}
	if len(compatible) != 1 {
		return PluginInventoryItem{}, fmt.Errorf("依赖 %s %s 的兼容候选数量为 %d", dependency.PluginID, dependency.Constraint, len(compatible))
	}
	return compatible[0], nil
}

func normalizeArtifactRef(ref ArtifactRef) ArtifactRef {
	if ref.Channel == "" {
		ref.Channel = "stable"
	}
	return ref
}
func validPluginTarget(value string) bool {
	return value == PluginTargetBackend || value == PluginTargetFrontend || value == PluginTargetRunner || value == PluginTargetMobile
}
