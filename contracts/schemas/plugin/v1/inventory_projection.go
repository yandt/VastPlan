package pluginv1

import (
	"encoding/json"
	"fmt"
	"sort"
)

func exactArtifactIdentity(value VerifiedArtifactManifest) (PluginArtifactIdentity, error) {
	artifact, manifest := value.Artifact, value.Manifest
	if artifact.PluginID == "" || artifact.PluginID != manifest.ID || artifact.Version != manifest.Version || artifact.Channel == "" || !isSHA256(artifact.SHA256) || manifest.Publisher == "" {
		return PluginArtifactIdentity{}, fmt.Errorf("制品与 Manifest 身份不一致: %s@%s", artifact.PluginID, artifact.Version)
	}
	return PluginArtifactIdentity{Ref: ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}, SHA256: artifact.SHA256, Publisher: manifest.Publisher}, nil
}

func inventoryItem(identity PluginArtifactIdentity, manifest Manifest) (PluginInventoryItem, error) {
	interfaceFingerprint, err := PublicInterfaceFingerprint(manifest)
	if err != nil {
		return PluginInventoryItem{}, err
	}
	targets := make([]PluginTarget, 0, len(manifest.Entry))
	for surface, entry := range manifest.Entry {
		driver := surface + "-module"
		if surface == "backend" {
			driver = BackendExecutionContract(manifest).Driver
		}
		targets = append(targets, PluginTarget{Surface: surface, Engine: manifest.Engines[surface], Entry: entry, Driver: driver})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Surface < targets[j].Surface })
	dependencies := make([]PluginDependency, 0, len(manifest.Dependencies))
	for id, constraint := range manifest.Dependencies {
		dependencies = append(dependencies, PluginDependency{PluginID: id, Constraint: constraint})
	}
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].PluginID < dependencies[j].PluginID })
	provides, requires := []RuntimeCapabilityPolicy{}, []RuntimeRequirement{}
	if manifest.Runtime != nil {
		provides = append(provides, manifest.Runtime.Provides...)
		requires = append(requires, manifest.Runtime.Requires...)
	}
	sort.Slice(provides, func(i, j int) bool {
		return provides[i].ExtensionPoint+"\x00"+provides[i].Capability < provides[j].ExtensionPoint+"\x00"+provides[j].Capability
	})
	sort.Slice(requires, func(i, j int) bool {
		return requires[i].Capability+"\x00"+requires[i].LogicalService < requires[j].Capability+"\x00"+requires[j].LogicalService
	})
	return PluginInventoryItem{Artifact: identity, InterfaceFingerprint: interfaceFingerprint, Targets: targets, Dependencies: dependencies, RuntimeProvides: provides, RuntimeRequires: requires, ConfigurationProtocols: configurationProtocols(manifest)}, nil
}

func configurationProtocols(manifest Manifest) []string {
	if manifest.Configuration == nil {
		return []string{}
	}
	values := []string{}
	if manifest.Configuration.Controller != nil {
		values = append(values, manifest.Configuration.Controller.Protocol)
	}
	if manifest.Configuration.ResourceController != nil {
		values = append(values, manifest.Configuration.ResourceController.Protocol)
	}
	sort.Strings(values)
	return values
}

func manifestContributions(owner PluginArtifactIdentity, manifest Manifest) ([]IndexedContribution, error) {
	result := []IndexedContribution{}
	for surface, raw := range manifest.Contributes {
		var groups map[string]json.RawMessage
		if err := json.Unmarshal(raw, &groups); err != nil {
			return nil, fmt.Errorf("解析插件 %s 的 %s 贡献: %w", manifest.ID, surface, err)
		}
		for group, groupRaw := range groups {
			var descriptors []json.RawMessage
			if err := json.Unmarshal(groupRaw, &descriptors); err != nil {
				return nil, fmt.Errorf("插件 %s 的贡献组 %s.%s 不是数组", manifest.ID, surface, group)
			}
			for _, descriptor := range descriptors {
				var header map[string]json.RawMessage
				if err := json.Unmarshal(descriptor, &header); err != nil || header == nil {
					continue
				}
				var id string
				if err := json.Unmarshal(header["id"], &id); err != nil || id == "" {
					return nil, fmt.Errorf("插件 %s 的贡献 %s.%s 缺少 id", manifest.ID, surface, group)
				}
				result = append(result, IndexedContribution{Kind: surface + "." + group, Surface: surface, ID: id, Contract: contributionContract(header), Owner: owner, Descriptor: append(json.RawMessage(nil), descriptor...)})
			}
		}
	}
	return result, nil
}

func contributionContract(fields map[string]json.RawMessage) string {
	for _, key := range []string{"uiContract", "engineContract", "interactionContract", "contract", "protocol"} {
		var value string
		if json.Unmarshal(fields[key], &value) == nil && value != "" {
			return value
		}
	}
	return ""
}
