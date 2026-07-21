package edge

import (
	"fmt"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func runtimeFrontendObjects(runtime portalapi.RuntimeSpec) []portalapi.FrontendModule {
	objects := append([]portalapi.FrontendModule(nil), runtime.Modules...)
	for _, graph := range runtime.ModuleGraphs {
		for _, node := range graph.Nodes {
			objects = append(objects, graphNodeDescriptor(graph, node))
		}
	}
	return objects
}

func cloneFrontendRuntime(runtime portalapi.RuntimeSpec) portalapi.RuntimeSpec {
	cloned := runtime
	cloned.Modules = append([]portalapi.FrontendModule(nil), runtime.Modules...)
	cloned.ModuleGraphs = make([]portalapi.FrontendModuleGraph, len(runtime.ModuleGraphs))
	for graphIndex, graph := range runtime.ModuleGraphs {
		cloned.ModuleGraphs[graphIndex] = cloneFrontendModuleGraph(graph)
	}
	return cloned
}

func cloneFrontendModuleGraph(graph portalapi.FrontendModuleGraph) portalapi.FrontendModuleGraph {
	cloned := graph
	cloned.Externals = append([]string{}, graph.Externals...)
	cloned.Nodes = make([]portalapi.FrontendModuleNode, len(graph.Nodes))
	for index, node := range graph.Nodes {
		clonedNode := node
		clonedNode.Dependencies = append([]portalapi.FrontendModuleDependency{}, node.Dependencies...)
		cloned.Nodes[index] = clonedNode
	}
	return cloned
}

func graphNodeDescriptor(graph portalapi.FrontendModuleGraph, node portalapi.FrontendModuleNode) portalapi.FrontendModule {
	return portalapi.FrontendModule{
		PluginRef: graph.PluginRef, Entry: graph.Entry, URL: node.URL, SHA256: node.SHA256,
		PackageSHA256: graph.PackageSHA256, Deferred: graph.Deferred, MediaType: node.MediaType,
	}
}

func findRuntimeFrontendObject(runtime portalapi.RuntimeSpec, digest string) portalapi.FrontendModule {
	for _, object := range runtimeFrontendObjects(runtime) {
		if object.SHA256 == digest {
			return object
		}
	}
	return portalapi.FrontendModule{}
}

func applyFrontendObjectURLs(runtime *portalapi.RuntimeSpec, urls map[string]string) error {
	for index := range runtime.Modules {
		digest := runtime.Modules[index].SHA256
		url := urls[digest]
		if url == "" {
			return fmt.Errorf("Portal RuntimeSpec 缺少内容对象: %s", digest)
		}
		runtime.Modules[index].URL = url
	}
	for graphIndex := range runtime.ModuleGraphs {
		for nodeIndex := range runtime.ModuleGraphs[graphIndex].Nodes {
			node := &runtime.ModuleGraphs[graphIndex].Nodes[nodeIndex]
			url := urls[node.SHA256]
			if url == "" {
				return fmt.Errorf("Portal Module Graph 缺少内容对象: %s/%s", runtime.ModuleGraphs[graphIndex].ID, node.Path)
			}
			node.URL = url
		}
	}
	return nil
}

func frontendObjectURL(revision uint64, digest, mediaType string) string {
	extension := ".bin"
	switch mediaType {
	case "text/javascript":
		extension = ".js"
	case "text/css":
		extension = ".css"
	case "application/json":
		extension = ".json"
	case "application/wasm":
		extension = ".wasm"
	}
	return fmt.Sprintf("/v1/portal-modules/%d/%s%s", revision, digest, extension)
}

func recoveryFrontendObjectURL(active, fallback uint64, regularURL string) string {
	name := regularURL[strings.LastIndex(regularURL, "/")+1:]
	return fmt.Sprintf("/v1/portal-recovery-modules/%d/%d/%s", active, fallback, name)
}
