package edge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func materializeFrontendModuleGraph(plugin verifiedPortalPlugin) (portalapi.FrontendModuleGraph, []FrontendModuleAsset, error) {
	source := plugin.manifest.FrontendModuleGraphs.Browser
	if source == nil {
		return portalapi.FrontendModuleGraph{}, nil, fmt.Errorf("插件 %s 缺少 browser Module Graph", plugin.ref.ID)
	}
	graph := portalapi.FrontendModuleGraph{
		PluginRef: plugin.ref, Target: source.Target, Entry: source.Entry, Digest: source.Digest,
		PackageSHA256: plugin.artifact.SHA256, Externals: append([]string(nil), source.Externals...),
		Nodes: make([]portalapi.FrontendModuleNode, 0, len(source.Nodes)), Deferred: isDeferredFrontendModule(plugin.manifest),
	}
	assets := make([]FrontendModuleAsset, 0, len(source.Nodes))
	for _, sourceNode := range source.Nodes {
		content, err := artifacttrust.ReadPackageFile(plugin.packageBytes, sourceNode.Path, sourceNode.Size)
		if err != nil {
			return portalapi.FrontendModuleGraph{}, nil, fmt.Errorf("读取插件 %s Module Graph 节点 %s: %w", plugin.ref.ID, sourceNode.Path, err)
		}
		digest := sha256.Sum256(content)
		if int64(len(content)) != sourceNode.Size || hex.EncodeToString(digest[:]) != sourceNode.SHA256 {
			return portalapi.FrontendModuleGraph{}, nil, fmt.Errorf("插件 %s Module Graph 节点字节失配: %s", plugin.ref.ID, sourceNode.Path)
		}
		node := portalapi.FrontendModuleNode{
			Path: sourceNode.Path, SHA256: sourceNode.SHA256, Size: sourceNode.Size,
			MediaType: sourceNode.MediaType, Purpose: sourceNode.Purpose,
			Dependencies: moduleDependencies(sourceNode.Dependencies),
		}
		graph.Nodes = append(graph.Nodes, node)
		assets = append(assets, FrontendModuleAsset{Descriptor: graphNodeDescriptor(graph, node), Content: content})
	}
	return graph, assets, nil
}

func moduleDependencies(source []pluginv1.FrontendModuleDependency) []portalapi.FrontendModuleDependency {
	target := make([]portalapi.FrontendModuleDependency, len(source))
	for index, dependency := range source {
		target[index] = portalapi.FrontendModuleDependency{Specifier: dependency.Specifier, Path: dependency.Path, Kind: dependency.Kind}
	}
	return target
}
