package servicemodel

import (
	"fmt"
	"sort"
)

// TopologicalOrder 返回依赖优先的稳定顺序。graph[node] 列出 node 必须先等待的节点。
// 缺失依赖和环都 fail-closed，避免“排序成功但启动永远悬空”。
func TopologicalOrder(graph map[string][]string) ([]string, error) {
	state := make(map[string]uint8, len(graph))
	order := make([]string, 0, len(graph))
	nodes := make([]string, 0, len(graph))
	for node := range graph {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	for _, node := range nodes {
		if _, ok := state[node]; !ok {
			if err := visitDependency(node, graph, state, &order); err != nil {
				return nil, err
			}
		}
	}
	return order, nil
}

func visitDependency(node string, graph map[string][]string, state map[string]uint8, order *[]string) error {
	switch state[node] {
	case 1:
		return fmt.Errorf("依赖图存在环，节点 %q 重复进入", node)
	case 2:
		return nil
	}
	if _, exists := graph[node]; !exists {
		return fmt.Errorf("依赖节点 %q 未声明", node)
	}
	state[node] = 1
	dependencies := append([]string(nil), graph[node]...)
	sort.Strings(dependencies)
	for _, dependency := range dependencies {
		if dependency == node {
			return fmt.Errorf("节点 %q 不能依赖自身", node)
		}
		if err := visitDependency(dependency, graph, state, order); err != nil {
			return err
		}
	}
	state[node] = 2
	*order = append(*order, node)
	return nil
}
