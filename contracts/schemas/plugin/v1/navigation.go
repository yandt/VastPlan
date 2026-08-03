package pluginv1

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	FrontendNavigationContributionKind = "frontend.navigations"
	FrontendNavigationContract         = "1.0.0"
	maxNavigationIconBytes             = 128 << 10
)

// FrontendNavigationCatalog is the signed, code-free navigation default owned
// by one frontend plugin. The trusted host binds every label to Manifest.ID.
type FrontendNavigationCatalog struct {
	ID       string                   `json:"id"`
	Contract string                   `json:"contract"`
	Nodes    []FrontendNavigationNode `json:"nodes"`
	Icons    []FrontendNavigationIcon `json:"icons"`
}

type FrontendNavigationNode struct {
	ID     string                    `json:"id"`
	Zone   string                    `json:"zone"`
	Label  FrontendNavigationLabel   `json:"label"`
	Icon   FrontendNavigationIconRef `json:"icon"`
	Parent *FrontendNavigationParent `json:"parent,omitempty"`
	Order  int                       `json:"order,omitempty"`
}

type FrontendNavigationLabel struct {
	Key      string `json:"key"`
	Fallback string `json:"fallback"`
}

type FrontendNavigationIconRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type FrontendNavigationParent struct {
	PluginID       string `json:"pluginId,omitempty"`
	NodeID         string `json:"nodeId"`
	Mode           string `json:"mode"`
	FallbackNodeID string `json:"fallbackNodeId,omitempty"`
}

type FrontendNavigationIcon struct {
	ID      string                     `json:"id"`
	States  map[string]json.RawMessage `json:"states,omitempty"`
	Sources map[string]string          `json:"sources,omitempty"`
	Motion  string                     `json:"motion"`
}

// FrontendNavigationCatalogFor returns the single validated navigation catalog
// from a Manifest. Absence is valid for frontend plugins without navigable UI.
func FrontendNavigationCatalogFor(manifest Manifest) (*FrontendNavigationCatalog, error) {
	raw := manifest.Contributes["frontend"]
	if len(raw) == 0 {
		return nil, nil
	}
	var frontend struct {
		Navigations []FrontendNavigationCatalog `json:"navigations"`
	}
	if err := json.Unmarshal(raw, &frontend); err != nil {
		return nil, fmt.Errorf("解析 frontend.navigations: %w", err)
	}
	if len(frontend.Navigations) == 0 {
		return nil, nil
	}
	if len(frontend.Navigations) != 1 {
		return nil, errors.New("frontend.navigations 每个插件最多声明一个目录")
	}
	catalog := frontend.Navigations[0]
	if err := validateFrontendNavigationCatalog(manifest, catalog); err != nil {
		return nil, err
	}
	return &catalog, nil
}

func validateFrontendNavigationCatalog(manifest Manifest, catalog FrontendNavigationCatalog) error {
	if _, frontend := manifest.Engines["frontend"]; !frontend || manifest.Entry["frontend"] == "" {
		return errors.New("frontend.navigations 只允许具有 frontend 入口的插件声明")
	}
	if catalog.ID != "main" || catalog.Contract != FrontendNavigationContract || len(catalog.Nodes) == 0 {
		return errors.New("frontend.navigations 目录身份、契约或节点无效")
	}
	nodes := make(map[string]FrontendNavigationNode, len(catalog.Nodes))
	for _, node := range catalog.Nodes {
		if _, duplicate := nodes[node.ID]; duplicate {
			return fmt.Errorf("frontend.navigations 节点重复: %s", node.ID)
		}
		nodes[node.ID] = node
	}
	icons := make(map[string]struct{}, len(catalog.Icons))
	iconBytes := 0
	for _, icon := range catalog.Icons {
		if _, duplicate := icons[icon.ID]; duplicate {
			return fmt.Errorf("frontend.navigations 图标重复: %s", icon.ID)
		}
		icons[icon.ID] = struct{}{}
		raw, _ := json.Marshal(icon)
		iconBytes += len(raw)
		if iconBytes > maxNavigationIconBytes {
			return errors.New("frontend.navigations 自定义图标目录超过 128 KiB")
		}
		if len(icon.Sources) > 0 {
			if len(icon.States) > 0 || icon.Sources["normal"] == "" {
				return fmt.Errorf("frontend.navigations 图标 %s 的 source 与 AST 状态不能混用", icon.ID)
			}
			for state, source := range icon.Sources {
				if !validNavigationIconState(state) || !validNavigationIconSource(source) {
					return fmt.Errorf("frontend.navigations 图标 %s 的 %s source 无效", icon.ID, state)
				}
			}
		} else {
			if len(icon.States) == 0 {
				return fmt.Errorf("frontend.navigations 图标 %s 缺少状态", icon.ID)
			}
			for state, glyph := range icon.States {
				if !validNavigationIconState(state) {
					return fmt.Errorf("frontend.navigations 图标 %s 的状态无效: %s", icon.ID, state)
				}
				if err := validateNavigationGlyphBudget(state, glyph); err != nil {
					return fmt.Errorf("frontend.navigations 图标 %s: %w", icon.ID, err)
				}
			}
		}
	}
	for _, node := range catalog.Nodes {
		if node.Icon.Kind == "custom" {
			if _, exists := icons[node.Icon.Name]; !exists {
				return fmt.Errorf("frontend.navigations 节点引用未知自定义图标: %s/%s", node.ID, node.Icon.Name)
			}
		}
		if err := validateNavigationParent(manifest, node, nodes); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePackagedNavigationCatalog rejects authoring-only SVG source paths.
// Every signed artifact must contain only the normalized, code-free glyph AST.
func ValidatePackagedNavigationCatalog(manifest Manifest) error {
	catalog, err := FrontendNavigationCatalogFor(manifest)
	if err != nil || catalog == nil {
		return err
	}
	for _, icon := range catalog.Icons {
		if len(icon.Sources) != 0 {
			return fmt.Errorf("签名插件制品不得保留原始 SVG source: %s", icon.ID)
		}
	}
	return nil
}

func validNavigationIconState(state string) bool {
	return state == "normal" || state == "active" || state == "loading" || state == "error"
}

func validNavigationIconSource(source string) bool {
	const prefix = "frontend/icons/navigation/"
	return len(source) > len(prefix)+4 && len(source) <= 240 && strings.HasPrefix(source, prefix) && strings.HasSuffix(source, ".svg") &&
		!strings.Contains(source, "..") && !strings.Contains(source, "\\") && !strings.Contains(source, "\x00") && !strings.Contains(source, "//")
}

func validateNavigationParent(manifest Manifest, node FrontendNavigationNode, nodes map[string]FrontendNavigationNode) error {
	parent := node.Parent
	if parent == nil {
		return nil
	}
	owner := parent.PluginID
	if owner == "" {
		owner = manifest.ID
	}
	if owner == manifest.ID {
		if parent.Mode != "required" || parent.FallbackNodeID != "" {
			return fmt.Errorf("同插件父级必须是 required 且不能声明回退: %s", node.ID)
		}
		candidate, exists := nodes[parent.NodeID]
		if !exists || parent.NodeID == node.ID {
			return fmt.Errorf("frontend.navigations 节点引用未知或自身父级: %s/%s", node.ID, parent.NodeID)
		}
		if candidate.Parent != nil {
			return fmt.Errorf("frontend.navigations 深度超过一级菜单、二级菜单、页面: %s", node.ID)
		}
		if candidate.Zone != node.Zone {
			return fmt.Errorf("frontend.navigations 子节点不能跨 zone: %s/%s", node.ID, parent.NodeID)
		}
		return nil
	}
	if owner == "vastplan.host" && parent.NodeID == "account" && parent.Mode == "required" && parent.FallbackNodeID == "" {
		return nil
	}
	if parent.Mode == "required" {
		if _, declared := manifest.Dependencies[owner]; !declared {
			return fmt.Errorf("frontend.navigations required 跨插件父级未声明依赖: %s/%s", node.ID, owner)
		}
		return nil
	}
	if parent.Mode != "optional" {
		return fmt.Errorf("frontend.navigations 跨插件父级模式无效: %s", node.ID)
	}
	fallback, exists := nodes[parent.FallbackNodeID]
	if !exists || fallback.Parent != nil || fallback.Zone != node.Zone || fallback.ID == node.ID {
		return fmt.Errorf("frontend.navigations optional 父级回退必须是同 zone 的自有根节点: %s/%s", node.ID, parent.FallbackNodeID)
	}
	return nil
}

func validateNavigationGlyphBudget(state string, raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > 32<<10 {
		return fmt.Errorf("%s 状态图元为空或超过 32 KiB", state)
	}
	var glyph struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	if json.Unmarshal(raw, &glyph) != nil || len(glyph.Nodes) == 0 {
		return fmt.Errorf("%s 状态图元无效", state)
	}
	return nil
}
