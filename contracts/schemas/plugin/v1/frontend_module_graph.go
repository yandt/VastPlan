package pluginv1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

type FrontendModuleGraphs struct {
	Browser *FrontendModuleGraph `json:"browser"`
	Server  *FrontendModuleGraph `json:"server,omitempty"`
}

type FrontendModuleGraph struct {
	SchemaVersion string               `json:"schemaVersion"`
	Target        string               `json:"target"`
	Entry         string               `json:"entry"`
	Digest        string               `json:"digest,omitempty"`
	Externals     []string             `json:"externals"`
	Nodes         []FrontendModuleNode `json:"nodes"`
}

type FrontendModuleNode struct {
	Path         string                     `json:"path"`
	SHA256       string                     `json:"sha256"`
	Size         int64                      `json:"size"`
	MediaType    string                     `json:"mediaType"`
	Purpose      string                     `json:"purpose"`
	Dependencies []FrontendModuleDependency `json:"dependencies"`
}

type FrontendModuleDependency struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

func (g FrontendModuleGraph) ComputedDigest() string {
	canonical := g
	canonical.Digest = ""
	canonical.Externals = append([]string{}, g.Externals...)
	sort.Strings(canonical.Externals)
	canonical.Nodes = append([]FrontendModuleNode{}, g.Nodes...)
	for index := range canonical.Nodes {
		canonical.Nodes[index].Dependencies = append([]FrontendModuleDependency{}, canonical.Nodes[index].Dependencies...)
		sort.Slice(canonical.Nodes[index].Dependencies, func(left, right int) bool {
			if canonical.Nodes[index].Dependencies[left].Path == canonical.Nodes[index].Dependencies[right].Path {
				return canonical.Nodes[index].Dependencies[left].Kind < canonical.Nodes[index].Dependencies[right].Kind
			}
			return canonical.Nodes[index].Dependencies[left].Path < canonical.Nodes[index].Dependencies[right].Path
		})
	}
	sort.Slice(canonical.Nodes, func(left, right int) bool { return canonical.Nodes[left].Path < canonical.Nodes[right].Path })
	raw, _ := json.Marshal(canonical)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func validateFrontendModuleGraphs(manifest Manifest) error {
	graphs := manifest.FrontendModuleGraphs
	if graphs == nil {
		return nil
	}
	if _, ok := manifest.Engines["frontend"]; !ok {
		return errors.New("frontendModuleGraphs 只能用于 Frontend 插件")
	}
	if graphs.Browser == nil || graphs.Browser.Target != "browser" || graphs.Browser.Entry != manifest.Entry["frontend"] {
		return errors.New("browser Module Graph 必须绑定 entry.frontend")
	}
	if err := validateFrontendModuleGraph(*graphs.Browser, 64<<20); err != nil {
		return fmt.Errorf("browser Module Graph 无效: %w", err)
	}
	if graphs.Server != nil {
		if graphs.Server.Target != "server" || graphs.Server.Entry != manifest.Entry["frontendServer"] {
			return errors.New("server Module Graph 必须绑定 entry.frontendServer")
		}
		if err := validateFrontendModuleGraph(*graphs.Server, 128<<20); err != nil {
			return fmt.Errorf("server Module Graph 无效: %w", err)
		}
	}
	return nil
}

func validateFrontendModuleGraph(graph FrontendModuleGraph, maxTotalSize int64) error {
	paths := make(map[string]FrontendModuleNode, len(graph.Nodes))
	digests := make(map[string]string, len(graph.Nodes))
	var totalSize int64
	for _, node := range graph.Nodes {
		if _, exists := paths[node.Path]; exists {
			return fmt.Errorf("节点路径重复: %s", node.Path)
		}
		if previous, exists := digests[node.SHA256]; exists {
			return fmt.Errorf("节点摘要重复映射: %s 与 %s", previous, node.Path)
		}
		paths[node.Path] = node
		digests[node.SHA256] = node.Path
		totalSize += node.Size
		if totalSize > maxTotalSize {
			return errors.New("节点总大小超过上限")
		}
	}
	entry, ok := paths[graph.Entry]
	if !ok || entry.Purpose != "entry" {
		return errors.New("入口节点缺失或 purpose 不是 entry")
	}
	for _, node := range graph.Nodes {
		dependencies := make(map[string]struct{}, len(node.Dependencies))
		for _, dependency := range node.Dependencies {
			key := dependency.Path + "\x00" + dependency.Kind
			if _, exists := dependencies[key]; exists {
				return fmt.Errorf("节点依赖重复: %s -> %s", node.Path, dependency.Path)
			}
			dependencies[key] = struct{}{}
			if dependency.Path == node.Path {
				return fmt.Errorf("节点不能依赖自身: %s", node.Path)
			}
			if _, exists := paths[dependency.Path]; !exists {
				return fmt.Errorf("节点依赖未闭合: %s -> %s", node.Path, dependency.Path)
			}
		}
	}
	if graph.Digest != graph.ComputedDigest() {
		return errors.New("Module Graph digest 与规范化内容不一致")
	}
	return nil
}
