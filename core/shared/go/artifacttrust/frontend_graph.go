package artifacttrust

import (
	"fmt"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type packageFileFact struct {
	size   int64
	sha256 string
}

func validatePackagedFrontendGraphs(manifest pluginv1.Manifest, files map[string]packageFileFact) error {
	if manifest.FrontendModuleGraphs == nil {
		return nil
	}
	if err := validatePackagedFrontendGraph("browser", manifest.FrontendModuleGraphs.Browser, files); err != nil {
		return err
	}
	if manifest.FrontendModuleGraphs.Server != nil {
		if err := validatePackagedFrontendGraph("server", manifest.FrontendModuleGraphs.Server, files); err != nil {
			return err
		}
	}
	return nil
}

func validatePackagedFrontendGraph(target string, graph *pluginv1.FrontendModuleGraph, files map[string]packageFileFact) error {
	if graph == nil {
		return fmt.Errorf("插件包缺少 %s Module Graph", target)
	}
	for _, node := range graph.Nodes {
		fact, ok := files[node.Path]
		if !ok {
			return fmt.Errorf("插件包缺少 %s Module Graph 节点 %s", target, node.Path)
		}
		if fact.size != node.Size {
			return fmt.Errorf("插件包 %s Module Graph 节点大小失配: %s", target, node.Path)
		}
		if fact.sha256 != node.SHA256 {
			return fmt.Errorf("插件包 %s Module Graph 节点摘要失配: %s", target, node.Path)
		}
	}
	return nil
}
