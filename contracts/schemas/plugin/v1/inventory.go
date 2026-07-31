package pluginv1

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const PluginInventorySchemaVersion = 1

// VerifiedArtifactManifest is the trusted input boundary for Inventory
// projection. Callers must obtain Artifact and Manifest from the same verified
// package; BuildPluginInventory checks their immutable identities again.
type VerifiedArtifactManifest struct {
	Artifact Artifact
	Manifest Manifest
}

// PluginArtifactIdentity is the exact, content-bound identity used by every
// kernel. A contribution never names a mutable latest version or source path.
type PluginArtifactIdentity struct {
	Ref       ArtifactRef `json:"ref"`
	SHA256    string      `json:"sha256"`
	Publisher string      `json:"publisher"`
}

type PluginTarget struct {
	Surface string `json:"surface"`
	Engine  string `json:"engine"`
	Entry   string `json:"entry"`
	Driver  string `json:"driver"`
}

type PluginDependency struct {
	PluginID   string `json:"pluginId"`
	Constraint string `json:"constraint"`
}

type PluginInventoryItem struct {
	Artifact               PluginArtifactIdentity    `json:"artifact"`
	Targets                []PluginTarget            `json:"targets"`
	Dependencies           []PluginDependency        `json:"dependencies"`
	RuntimeProvides        []RuntimeCapabilityPolicy `json:"runtimeProvides"`
	RuntimeRequires        []RuntimeRequirement      `json:"runtimeRequires"`
	ConfigurationProtocols []string                  `json:"configurationProtocols"`
}

// PluginInventorySnapshot is a deterministic projection of one admitted exact
// plugin set. SourceDigest binds the upstream Catalog/Composition selection;
// Digest binds this normalized projection itself.
type PluginInventorySnapshot struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Generation    uint64                `json:"generation"`
	SourceDigest  string                `json:"sourceDigest"`
	Plugins       []PluginInventoryItem `json:"plugins"`
	Digest        string                `json:"digest"`
}

// IndexedContribution keeps the signed JSON descriptor intact. Consumers query
// Kind and Surface, then validate the descriptor against the contract they own;
// adding a new contribution group does not require changing this projection.
type IndexedContribution struct {
	Kind       string                 `json:"kind"`
	Surface    string                 `json:"surface"`
	ID         string                 `json:"id"`
	Contract   string                 `json:"contract,omitempty"`
	Owner      PluginArtifactIdentity `json:"owner"`
	Descriptor json.RawMessage        `json:"descriptor"`
}

type ContributionIndexSnapshot struct {
	SchemaVersion   int                   `json:"schemaVersion"`
	Generation      uint64                `json:"generation"`
	InventoryDigest string                `json:"inventoryDigest"`
	Contributions   []IndexedContribution `json:"contributions"`
	Digest          string                `json:"digest"`
}

// BuildPluginInventory projects already verified manifests into a stable
// cross-kernel inventory. Directory layout and repository implementation never
// enter the result.
func BuildPluginInventory(generation uint64, sourceDigest string, values []VerifiedArtifactManifest) (PluginInventorySnapshot, error) {
	if generation == 0 {
		return PluginInventorySnapshot{}, errors.New("Plugin Inventory generation 必须大于 0")
	}
	if !isSHA256(sourceDigest) {
		return PluginInventorySnapshot{}, errors.New("Plugin Inventory sourceDigest 无效")
	}
	items := make([]PluginInventoryItem, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		identity, err := exactArtifactIdentity(value)
		if err != nil {
			return PluginInventorySnapshot{}, err
		}
		key := artifactIdentityKey(identity)
		if _, duplicate := seen[key]; duplicate {
			return PluginInventorySnapshot{}, fmt.Errorf("Plugin Inventory 制品重复: %s", key)
		}
		seen[key] = struct{}{}
		items = append(items, inventoryItem(identity, value.Manifest))
	}
	sort.Slice(items, func(i, j int) bool {
		return artifactIdentityKey(items[i].Artifact) < artifactIdentityKey(items[j].Artifact)
	})
	snapshot := PluginInventorySnapshot{SchemaVersion: PluginInventorySchemaVersion, Generation: generation, SourceDigest: sourceDigest, Plugins: items}
	digest, err := pluginInventoryDigest(snapshot)
	if err != nil {
		return PluginInventorySnapshot{}, err
	}
	snapshot.Digest = digest
	return snapshot, nil
}

// BuildContributionIndex projects every Manifest contribution group without a
// family allow-list. Unknown kinds stay discoverable but remain inert until a
// compatible consumer validates and activates them.
func BuildContributionIndex(inventory PluginInventorySnapshot, values []VerifiedArtifactManifest) (ContributionIndexSnapshot, error) {
	if err := ValidatePluginInventory(inventory); err != nil {
		return ContributionIndexSnapshot{}, err
	}
	ownerByKey := make(map[string]PluginArtifactIdentity, len(inventory.Plugins))
	for _, item := range inventory.Plugins {
		ownerByKey[artifactIdentityKey(item.Artifact)] = item.Artifact
	}
	contributions := make([]IndexedContribution, 0)
	seen := map[string]struct{}{}
	for _, value := range values {
		identity, err := exactArtifactIdentity(value)
		if err != nil {
			return ContributionIndexSnapshot{}, err
		}
		owner, exists := ownerByKey[artifactIdentityKey(identity)]
		if !exists {
			return ContributionIndexSnapshot{}, fmt.Errorf("Contribution Index 所有者不在 Inventory: %s", artifactIdentityKey(identity))
		}
		projected, err := manifestContributions(owner, value.Manifest)
		if err != nil {
			return ContributionIndexSnapshot{}, err
		}
		for _, contribution := range projected {
			key := contribution.Kind + "\x00" + contribution.ID + "\x00" + artifactIdentityKey(contribution.Owner)
			if _, duplicate := seen[key]; duplicate {
				return ContributionIndexSnapshot{}, fmt.Errorf("Contribution Index 身份重复: %s/%s", contribution.Kind, contribution.ID)
			}
			seen[key] = struct{}{}
			contributions = append(contributions, contribution)
		}
	}
	sort.Slice(contributions, func(i, j int) bool {
		left, right := contributions[i], contributions[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return artifactIdentityKey(left.Owner) < artifactIdentityKey(right.Owner)
	})
	index := ContributionIndexSnapshot{SchemaVersion: PluginInventorySchemaVersion, Generation: inventory.Generation, InventoryDigest: inventory.Digest, Contributions: contributions}
	digest, err := contributionIndexDigest(index)
	if err != nil {
		return ContributionIndexSnapshot{}, err
	}
	index.Digest = digest
	return index, nil
}

func ValidatePluginInventory(snapshot PluginInventorySnapshot) error {
	if snapshot.SchemaVersion != PluginInventorySchemaVersion || snapshot.Generation == 0 || !isSHA256(snapshot.SourceDigest) || !isSHA256(snapshot.Digest) {
		return errors.New("Plugin Inventory 快照身份无效")
	}
	expected, err := pluginInventoryDigest(snapshot)
	if err != nil || expected != snapshot.Digest {
		return errors.New("Plugin Inventory 摘要失配")
	}
	return nil
}

func ValidateContributionIndex(index ContributionIndexSnapshot) error {
	if index.SchemaVersion != PluginInventorySchemaVersion || index.Generation == 0 || !isSHA256(index.InventoryDigest) || !isSHA256(index.Digest) {
		return errors.New("Contribution Index 快照身份无效")
	}
	expected, err := contributionIndexDigest(index)
	if err != nil || expected != index.Digest {
		return errors.New("Contribution Index 摘要失配")
	}
	seen := map[string]struct{}{}
	for _, contribution := range index.Contributions {
		prefix := contribution.Surface + "."
		if contribution.Surface == "" || !strings.HasPrefix(contribution.Kind, prefix) || len(contribution.Kind) == len(prefix) || contribution.ID == "" ||
			contribution.Owner.Ref.PluginID == "" || contribution.Owner.Ref.Version == "" || contribution.Owner.Ref.Channel == "" || contribution.Owner.Publisher == "" || !isSHA256(contribution.Owner.SHA256) || len(contribution.Descriptor) == 0 {
			return errors.New("Contribution Index 贡献无效")
		}
		var descriptor map[string]json.RawMessage
		var descriptorID string
		if json.Unmarshal(contribution.Descriptor, &descriptor) != nil || descriptor == nil || json.Unmarshal(descriptor["id"], &descriptorID) != nil || descriptorID != contribution.ID {
			return errors.New("Contribution Index descriptor 身份无效")
		}
		key := contribution.Kind + "\x00" + contribution.ID + "\x00" + artifactIdentityKey(contribution.Owner)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("Contribution Index 身份重复: %s/%s", contribution.Kind, contribution.ID)
		}
		seen[key] = struct{}{}
	}
	return nil
}
