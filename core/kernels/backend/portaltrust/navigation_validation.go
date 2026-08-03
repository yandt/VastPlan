package portaltrust

import (
	"encoding/json"
	"fmt"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

type portalNavigationNode struct {
	id       string
	zone     string
	parent   string
	fallback string
	optional bool
}

// validateNavigationCandidates is the trusted-host publication gate for the
// final plugin catalog plus Portal overrides. The browser repeats these checks
// defensively, but it is never the first authority to reject an invalid tree.
func validateNavigationCandidates(spec portalapi.PortalSpec, index pluginv1.ContributionIndexSnapshot) error {
	nodes := make(map[string]portalNavigationNode)
	for _, contribution := range index.Contributions {
		if contribution.Kind != pluginv1.FrontendNavigationContributionKind {
			continue
		}
		var catalog pluginv1.FrontendNavigationCatalog
		if err := json.Unmarshal(contribution.Descriptor, &catalog); err != nil || contribution.ID != "main" || contribution.Contract != pluginv1.FrontendNavigationContract || catalog.ID != contribution.ID || catalog.Contract != contribution.Contract {
			return fmt.Errorf("导航 Contribution 无效: %s/%s", contribution.Owner.Ref.PluginID, contribution.ID)
		}
		for _, candidate := range catalog.Nodes {
			id := navigationNodeID(contribution.Owner.Ref.PluginID, candidate.ID)
			if _, duplicate := nodes[id]; duplicate {
				return fmt.Errorf("Portal 导航节点全局身份重复: %s", id)
			}
			node := portalNavigationNode{id: id, zone: candidate.Zone}
			if candidate.Parent != nil {
				owner := candidate.Parent.PluginID
				if owner == "" {
					owner = contribution.Owner.Ref.PluginID
				}
				parentID := navigationNodeID(owner, candidate.Parent.NodeID)
				if parentID == "vastplan.host/account" {
					if candidate.Parent.Mode != "required" || candidate.Zone != "secondary" {
						return fmt.Errorf("账户头像菜单必须位于 secondary 区域且使用 required 父级: %s", contribution.Owner.Ref.PluginID)
					}
				} else {
					node.parent = parentID
					node.optional = candidate.Parent.Mode == "optional"
					if node.optional {
						node.fallback = navigationNodeID(contribution.Owner.Ref.PluginID, candidate.Parent.FallbackNodeID)
					}
				}
			}
			nodes[id] = node
		}
	}
	for _, override := range spec.Shell.Config.NavigationOverrides {
		node, exists := nodes[override.Target]
		if !exists {
			return fmt.Errorf("Portal 导航覆盖引用未安装节点: %s", override.Target)
		}
		if override.Parent != "" {
			if _, exists := nodes[override.Parent]; !exists {
				return fmt.Errorf("Portal 导航覆盖引用未安装父级: %s/%s", override.Target, override.Parent)
			}
			node.parent, node.fallback, node.optional = override.Parent, "", false
			nodes[override.Target] = node
		}
	}
	for _, node := range nodes {
		parent, hasParent, err := resolvedNavigationParent(node, nodes)
		if err != nil {
			return err
		}
		if !hasParent {
			continue
		}
		if parent.zone != node.zone {
			return fmt.Errorf("Portal 导航不能跨 zone: %s/%s", node.id, parent.id)
		}
		if _, parentHasParent, parentErr := resolvedNavigationParent(parent, nodes); parentErr != nil {
			return parentErr
		} else if parentHasParent {
			return fmt.Errorf("Portal 导航循环或深度超过一级菜单、二级菜单、页面: %s", node.id)
		}
	}
	return nil
}

func resolvedNavigationParent(node portalNavigationNode, nodes map[string]portalNavigationNode) (portalNavigationNode, bool, error) {
	if node.parent == "" {
		return portalNavigationNode{}, false, nil
	}
	if parent, exists := nodes[node.parent]; exists {
		if parent.id == node.id {
			return portalNavigationNode{}, false, fmt.Errorf("Portal 导航节点不能引用自身: %s", node.id)
		}
		return parent, true, nil
	}
	if node.optional {
		if fallback, exists := nodes[node.fallback]; exists && fallback.id != node.id {
			return fallback, true, nil
		}
	}
	return portalNavigationNode{}, false, fmt.Errorf("Portal 导航引用未知父级: %s/%s", node.id, node.parent)
}

func navigationNodeID(pluginID, nodeID string) string {
	return pluginID + "/" + nodeID
}
