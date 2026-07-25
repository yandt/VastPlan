package backendcompositionv1

import (
	"fmt"
	"sort"
)

func normalizeServiceGraph(graph *ServiceDependencyGraph) error {
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].UnitID < graph.Nodes[j].UnitID })
	nodes := make(map[string]struct{}, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if _, duplicate := nodes[node.UnitID]; duplicate {
			return fmt.Errorf("Resolution Report service graph node 重复: %q", node.UnitID)
		}
		nodes[node.UnitID] = struct{}{}
	}
	sort.Slice(graph.Edges, func(i, j int) bool {
		left, right := graph.Edges[i], graph.Edges[j]
		if left.FromUnitID != right.FromUnitID {
			return left.FromUnitID < right.FromUnitID
		}
		if left.ToUnitID != right.ToUnitID {
			return left.ToUnitID < right.ToUnitID
		}
		return left.Capability < right.Capability
	})
	edges := map[string]struct{}{}
	adjacency := map[string][]string{}
	for _, edge := range graph.Edges {
		if _, ok := nodes[edge.FromUnitID]; !ok {
			return fmt.Errorf("Resolution Report service graph edge 引用未知 consumer unit %q", edge.FromUnitID)
		}
		if _, ok := nodes[edge.ToUnitID]; !ok {
			return fmt.Errorf("Resolution Report service graph edge 引用未知 provider unit %q", edge.ToUnitID)
		}
		if edge.FromUnitID == edge.ToUnitID {
			return fmt.Errorf("Resolution Report service graph 不允许自环: %q", edge.FromUnitID)
		}
		key := edge.FromUnitID + "\x00" + edge.ToUnitID + "\x00" + edge.Capability
		if _, duplicate := edges[key]; duplicate {
			return fmt.Errorf("Resolution Report service graph edge 重复: %s -> %s (%s)", edge.FromUnitID, edge.ToUnitID, edge.Capability)
		}
		edges[key] = struct{}{}
		adjacency[edge.FromUnitID] = append(adjacency[edge.FromUnitID], edge.ToUnitID)
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(node string) bool {
		if visiting[node] {
			return true
		}
		if visited[node] {
			return false
		}
		visiting[node] = true
		for _, next := range adjacency[node] {
			if visit(next) {
				return true
			}
		}
		visiting[node] = false
		visited[node] = true
		return false
	}
	for node := range nodes {
		if visit(node) {
			return fmt.Errorf("Resolution Report service graph 必须是 DAG")
		}
	}
	return nil
}
