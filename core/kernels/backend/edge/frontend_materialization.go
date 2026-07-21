package edge

import (
	"fmt"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

// materializeFrontendRuntime converts already verified packages into one
// immutable browser RuntimeSpec. Package verification remains in Catalog;
// graph projection and object encoding remain in their dedicated adapters.
func materializeFrontendRuntime(spec portalapi.PortalSpec, verified []verifiedPortalPlugin) (portalapi.RuntimeSpec, []FrontendModuleAsset, []pluginv1.ArtifactReference, error) {
	runtime := portalapi.RuntimeSpec{Portal: spec}
	assets := make([]FrontendModuleAsset, 0, len(spec.Plugins))
	references := make([]pluginv1.ArtifactReference, 0, len(spec.Plugins))
	for _, plugin := range verified {
		if plugin.manifest.FrontendModuleGraphs == nil {
			asset, err := frontendModule(spec.Revision, plugin)
			if err != nil {
				return portalapi.RuntimeSpec{}, nil, nil, err
			}
			assets = append(assets, asset)
			runtime.Modules = append(runtime.Modules, asset.Descriptor)
		} else {
			graph, graphAssets, err := materializeFrontendModuleGraph(plugin)
			if err != nil {
				return portalapi.RuntimeSpec{}, nil, nil, err
			}
			runtime.ModuleGraphs = append(runtime.ModuleGraphs, graph)
			assets = append(assets, graphAssets...)
		}
		references = append(references, artifactReference(plugin))
	}
	for index := range assets {
		compressed, err := gzipBytes(assets[index].Content)
		if err != nil {
			return portalapi.RuntimeSpec{}, nil, nil, fmt.Errorf("压缩插件 %s frontend 对象: %w", assets[index].Descriptor.ID, err)
		}
		assets[index].GzipContent = compressed
	}
	return runtime, assets, references, nil
}

func artifactReference(plugin verifiedPortalPlugin) pluginv1.ArtifactReference {
	selectedChannel := plugin.ref.Channel
	if selectedChannel == "" {
		selectedChannel = "stable"
	}
	return pluginv1.ArtifactReference{
		Ref:    pluginv1.ArtifactRef{PluginID: plugin.ref.ID, Version: plugin.ref.Version, Channel: selectedChannel},
		SHA256: plugin.artifact.SHA256, Purpose: "candidate",
	}
}
