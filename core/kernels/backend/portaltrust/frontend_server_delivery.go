package portaltrust

import (
	"fmt"

	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

// serverRuntimeSpec is sealed inside the trusted delivery snapshot. It is
// intentionally absent from portalapi.RuntimeSpec and every browser response.
type serverRuntimeSpec struct {
	ModuleGraphs []portalapi.FrontendModuleGraph `json:"moduleGraphs,omitempty"`
}

func cloneServerRuntime(runtime serverRuntimeSpec) serverRuntimeSpec {
	cloned := serverRuntimeSpec{ModuleGraphs: make([]portalapi.FrontendModuleGraph, len(runtime.ModuleGraphs))}
	for index, graph := range runtime.ModuleGraphs {
		cloned.ModuleGraphs[index] = cloneFrontendModuleGraph(graph)
	}
	return cloned
}

func serverRuntimeObjects(runtime serverRuntimeSpec) []portalapi.FrontendModule {
	objects := make([]portalapi.FrontendModule, 0)
	for _, graph := range runtime.ModuleGraphs {
		for _, node := range graph.Nodes {
			objects = append(objects, graphNodeDescriptor(graph, node))
		}
	}
	return objects
}

func applyServerObjectURLs(runtime *serverRuntimeSpec, urls map[string]string) error {
	for graphIndex := range runtime.ModuleGraphs {
		for nodeIndex := range runtime.ModuleGraphs[graphIndex].Nodes {
			node := &runtime.ModuleGraphs[graphIndex].Nodes[nodeIndex]
			url := urls[node.SHA256]
			if url == "" {
				return fmt.Errorf("Portal Server Module Graph 缺少内容对象: %s/%s", runtime.ModuleGraphs[graphIndex].ID, node.Path)
			}
			node.URL = url
		}
	}
	return nil
}

func serverObjectURL(digest string) string { return "server-object:" + digest }
