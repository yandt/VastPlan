package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type frontendHMRContribution struct {
	Kind       string
	Surface    string
	ID         string
	Contract   string
	Descriptor json.RawMessage
}

func readFrontendHMRContributions(root, pluginID string) (string, []frontendHMRContribution, error) {
	pluginPath, err := developmentFrontendPluginPath(root, pluginID)
	if err != nil {
		return "", nil, err
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pluginPath), "vastplan.plugin.json"))
	if err != nil {
		return "", nil, err
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		return "", nil, err
	}
	var groups map[string]json.RawMessage
	if err := json.Unmarshal(manifest.Contributes["frontend"], &groups); err != nil {
		return "", nil, err
	}
	values := []frontendHMRContribution{}
	for group, groupRaw := range groups {
		var descriptors []json.RawMessage
		if err := json.Unmarshal(groupRaw, &descriptors); err != nil {
			return "", nil, fmt.Errorf("frontend.%s 不是贡献数组", group)
		}
		for _, descriptor := range descriptors {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(descriptor, &fields); err != nil || fields == nil {
				continue
			}
			var id string
			if json.Unmarshal(fields["id"], &id) != nil || id == "" {
				return "", nil, fmt.Errorf("frontend.%s 贡献缺少 id", group)
			}
			values = append(values, frontendHMRContribution{Kind: "frontend." + group, Surface: "frontend", ID: id, Contract: frontendHMRContributionContract(fields), Descriptor: append(json.RawMessage(nil), descriptor...)})
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Kind != values[j].Kind {
			return values[i].Kind < values[j].Kind
		}
		return values[i].ID < values[j].ID
	})
	return manifest.Publisher, values, nil
}

func overlayFrontendHMRContributions(document map[string]json.RawMessage, modules map[string]frontendHMRModule) error {
	raw, exists := document["contributions"]
	if !exists {
		return nil
	}
	var index pluginv1.ContributionIndexSnapshot
	if err := json.Unmarshal(raw, &index); err != nil || index.SchemaVersion != pluginv1.PluginInventorySchemaVersion || index.Generation == 0 {
		return errors.New("RuntimeSpec Contribution Index 无效")
	}
	owners, err := frontendHMRRuntimeOwners(document)
	if err != nil {
		return err
	}
	kept := make([]pluginv1.IndexedContribution, 0, len(index.Contributions))
	for _, contribution := range index.Contributions {
		if _, overlaid := modules[contribution.Owner.Ref.PluginID]; overlaid {
			continue
		}
		kept = append(kept, contribution)
	}
	for id, module := range modules {
		owner, selected := owners[id]
		if !selected {
			continue
		}
		owner.Publisher = module.Publisher
		for _, contribution := range module.Contributions {
			kept = append(kept, pluginv1.IndexedContribution{Kind: contribution.Kind, Surface: contribution.Surface, ID: contribution.ID, Contract: contribution.Contract, Owner: owner, Descriptor: append(json.RawMessage(nil), contribution.Descriptor...)})
		}
	}
	sort.Slice(kept, func(i, j int) bool {
		left, right := kept[i], kept[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return left.Owner.Ref.PluginID < right.Owner.Ref.PluginID
	})
	index.Contributions = kept
	index.InventoryDigest = frontendHMRJSONDigest(struct {
		Generation uint64                                     `json:"generation"`
		Owners     map[string]pluginv1.PluginArtifactIdentity `json:"owners"`
	}{index.Generation, owners})
	index.Digest = frontendHMRJSONDigest(struct {
		SchemaVersion   int                            `json:"schemaVersion"`
		Generation      uint64                         `json:"generation"`
		InventoryDigest string                         `json:"inventoryDigest"`
		Contributions   []pluginv1.IndexedContribution `json:"contributions"`
	}{index.SchemaVersion, index.Generation, index.InventoryDigest, index.Contributions})
	encoded, err := json.Marshal(index)
	if err != nil {
		return err
	}
	document["contributions"] = encoded
	return nil
}

func frontendHMRRuntimeOwners(document map[string]json.RawMessage) (map[string]pluginv1.PluginArtifactIdentity, error) {
	owners := map[string]pluginv1.PluginArtifactIdentity{}
	for _, key := range []string{"modules", "moduleGraphs"} {
		if len(document[key]) == 0 {
			continue
		}
		var values []struct{ ID, Version, Channel, PackageSHA256 string }
		if err := json.Unmarshal(document[key], &values); err != nil {
			return nil, fmt.Errorf("RuntimeSpec %s 无效", key)
		}
		for _, value := range values {
			channel := value.Channel
			if channel == "" {
				channel = "stable"
			}
			if value.ID == "" || value.Version == "" || len(value.PackageSHA256) != 64 {
				return nil, fmt.Errorf("RuntimeSpec %s 制品身份无效", key)
			}
			identity := pluginv1.PluginArtifactIdentity{Ref: pluginv1.ArtifactRef{PluginID: value.ID, Version: value.Version, Channel: channel}, SHA256: value.PackageSHA256}
			if existing, duplicate := owners[value.ID]; duplicate && (existing.Ref != identity.Ref || existing.SHA256 != identity.SHA256) {
				return nil, fmt.Errorf("RuntimeSpec 插件身份冲突: %s", value.ID)
			}
			owners[value.ID] = identity
		}
	}
	return owners, nil
}

func frontendHMRContributionContract(fields map[string]json.RawMessage) string {
	for _, key := range []string{"uiContract", "engineContract", "interactionContract", "contract", "protocol"} {
		var value string
		if json.Unmarshal(fields[key], &value) == nil && value != "" {
			return value
		}
	}
	return ""
}

func frontendHMRJSONDigest(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
