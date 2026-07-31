package pluginv1

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const FrontendPageExtensionKind = "frontend.page"

// ExtensionPointDeclaration is owned by one signed plugin manifest. The point
// defines a data contract; it never exports language objects or implementation
// callbacks across plugin boundaries.
type ExtensionPointDeclaration struct {
	ID               string          `json:"id"`
	Surface          string          `json:"surface"`
	Contract         string          `json:"contract"`
	Kind             string          `json:"kind"`
	Dispatch         string          `json:"dispatch"`
	Targets          []string        `json:"targets,omitempty"`
	DescriptorSchema json.RawMessage `json:"descriptorSchema"`
}

// ExtensionContribution binds one plugin-owned, JSON-only descriptor to an
// extension point. Runtime code is still loaded from the contributor's own
// signed module and retains that plugin's permissions and lifecycle.
type ExtensionContribution struct {
	Point      string          `json:"point"`
	Surface    string          `json:"surface"`
	Contract   string          `json:"contract"`
	ID         string          `json:"id"`
	Order      int             `json:"order,omitempty"`
	Descriptor json.RawMessage `json:"descriptor"`
}

type ResolvedExtensionPoint struct {
	ID            string   `json:"id"`
	OwnerPluginID string   `json:"ownerPluginId"`
	Surface       string   `json:"surface"`
	Contract      string   `json:"contract"`
	Kind          string   `json:"kind"`
	Dispatch      string   `json:"dispatch"`
	Targets       []string `json:"targets,omitempty"`
}

type ResolvedExtensionContribution struct {
	Point      string          `json:"point"`
	ID         string          `json:"id"`
	PluginID   string          `json:"pluginId"`
	Contract   string          `json:"contract"`
	Order      int             `json:"order,omitempty"`
	Descriptor json.RawMessage `json:"descriptor"`
}

type ExtensionGraph struct {
	Points        []ResolvedExtensionPoint        `json:"points"`
	Contributions []ResolvedExtensionContribution `json:"contributions"`
}

func validatePluginExtensions(manifest Manifest) error {
	pointIDs := make(map[string]struct{}, len(manifest.ExtensionPoints))
	for _, point := range manifest.ExtensionPoints {
		if !strings.HasPrefix(point.ID, manifest.ID+".") {
			return fmt.Errorf("extensionPoints %q 必须位于插件命名空间 %s.*", point.ID, manifest.ID)
		}
		if _, duplicate := pointIDs[point.ID]; duplicate {
			return fmt.Errorf("extensionPoints id 重复: %q", point.ID)
		}
		pointIDs[point.ID] = struct{}{}
		if manifest.Engines[point.Surface] == "" || manifest.Entry[point.Surface] == "" {
			return fmt.Errorf("extensionPoints %q 的 surface %s 没有对应 Engine 和入口", point.ID, point.Surface)
		}
		if _, err := semver.NewVersion(point.Contract); err != nil {
			return fmt.Errorf("extensionPoints %q contract 无效: %w", point.ID, err)
		}
		if err := validateExtensionDescriptorSchema(point.ID, point.DescriptorSchema); err != nil {
			return err
		}
		if point.Kind == FrontendPageExtensionKind && len(point.Targets) == 0 {
			return fmt.Errorf("extensionPoints %q 的 frontend.page 必须声明 targets", point.ID)
		}
	}

	extensionIDs := make(map[string]struct{}, len(manifest.Extensions))
	for _, extension := range manifest.Extensions {
		key := extension.Point + "\x00" + extension.ID
		if _, duplicate := extensionIDs[key]; duplicate {
			return fmt.Errorf("extensions id 重复: %s/%s", extension.Point, extension.ID)
		}
		extensionIDs[key] = struct{}{}
		if !strings.HasPrefix(extension.ID, manifest.ID+".") {
			return fmt.Errorf("extensions id %q 必须位于插件命名空间 %s.*", extension.ID, manifest.ID)
		}
		if manifest.Engines[extension.Surface] == "" || manifest.Entry[extension.Surface] == "" {
			return fmt.Errorf("extensions %q 的 surface %s 没有对应 Engine 和入口", extension.ID, extension.Surface)
		}
		if _, err := semver.NewConstraint(extension.Contract); err != nil {
			return fmt.Errorf("extensions %q contract 无效: %w", extension.ID, err)
		}
		var descriptor map[string]any
		if err := json.Unmarshal(extension.Descriptor, &descriptor); err != nil || descriptor == nil {
			return fmt.Errorf("extensions %q descriptor 必须是 JSON 对象", extension.ID)
		}
	}
	return nil
}

// ResolveExtensionGraph validates a set of already verified manifests and
// returns the immutable graph for one kernel surface. Missing owners,
// incompatible contracts and invalid descriptors are publication failures.
func ResolveExtensionGraph(manifests []Manifest, surface string) (ExtensionGraph, error) {
	if surface == "" {
		return ExtensionGraph{}, errors.New("扩展图 surface 不能为空")
	}
	manifestByID := make(map[string]Manifest, len(manifests))
	pointByID := map[string]struct {
		owner Manifest
		point ExtensionPointDeclaration
	}{}
	for _, manifest := range manifests {
		if _, duplicate := manifestByID[manifest.ID]; duplicate {
			return ExtensionGraph{}, fmt.Errorf("扩展图插件重复: %s", manifest.ID)
		}
		manifestByID[manifest.ID] = manifest
		for _, point := range manifest.ExtensionPoints {
			if point.Surface != surface {
				continue
			}
			if _, duplicate := pointByID[point.ID]; duplicate {
				return ExtensionGraph{}, fmt.Errorf("扩展点重复: %s", point.ID)
			}
			pointByID[point.ID] = struct {
				owner Manifest
				point ExtensionPointDeclaration
			}{owner: manifest, point: point}
		}
	}

	graph := ExtensionGraph{Points: make([]ResolvedExtensionPoint, 0, len(pointByID)), Contributions: []ResolvedExtensionContribution{}}
	targetOwners := map[string]string{}
	for _, value := range pointByID {
		point := value.point
		for _, target := range point.Targets {
			key := point.Kind + "\x00" + target
			if owner, exists := targetOwners[key]; exists && owner != value.owner.ID {
				return ExtensionGraph{}, fmt.Errorf("扩展目标 %s/%s 被多个插件拥有", point.Kind, target)
			}
			targetOwners[key] = value.owner.ID
		}
		graph.Points = append(graph.Points, ResolvedExtensionPoint{
			ID: point.ID, OwnerPluginID: value.owner.ID, Surface: point.Surface,
			Contract: point.Contract, Kind: point.Kind, Dispatch: point.Dispatch,
			Targets: append([]string(nil), point.Targets...),
		})
	}

	counts := map[string]int{}
	for _, contributor := range manifests {
		for _, extension := range contributor.Extensions {
			if extension.Surface != surface {
				continue
			}
			owned, exists := pointByID[extension.Point]
			if !exists {
				return ExtensionGraph{}, fmt.Errorf("插件 %s 引用了未装配的扩展点 %s", contributor.ID, extension.Point)
			}
			if contributor.ID == owned.owner.ID {
				return ExtensionGraph{}, fmt.Errorf("扩展点所有者 %s 不应通过 extensions 扩展自身", contributor.ID)
			}
			dependency, declared := contributor.Dependencies[owned.owner.ID]
			if !declared {
				return ExtensionGraph{}, fmt.Errorf("插件 %s 扩展 %s 前必须依赖所有者 %s", contributor.ID, extension.Point, owned.owner.ID)
			}
			if err := checkVersionRange(dependency, owned.owner.Version); err != nil {
				return ExtensionGraph{}, fmt.Errorf("插件 %s 对扩展点所有者 %s 的依赖不兼容: %w", contributor.ID, owned.owner.ID, err)
			}
			if err := checkVersionRange(extension.Contract, owned.point.Contract); err != nil {
				return ExtensionGraph{}, fmt.Errorf("插件 %s 对扩展点 %s 的契约不兼容: %w", contributor.ID, extension.Point, err)
			}
			if err := validateExtensionDescriptor(owned.point, extension.Descriptor); err != nil {
				return ExtensionGraph{}, fmt.Errorf("插件 %s 扩展 %s: %w", contributor.ID, extension.Point, err)
			}
			counts[extension.Point]++
			if owned.point.Dispatch == "single" && counts[extension.Point] > 1 {
				return ExtensionGraph{}, fmt.Errorf("single 扩展点 %s 只能有一个贡献", extension.Point)
			}
			graph.Contributions = append(graph.Contributions, ResolvedExtensionContribution{
				Point: extension.Point, ID: extension.ID, PluginID: contributor.ID,
				Contract: extension.Contract, Order: extension.Order,
				Descriptor: append(json.RawMessage(nil), extension.Descriptor...),
			})
		}
	}
	sort.Slice(graph.Points, func(i, j int) bool { return graph.Points[i].ID < graph.Points[j].ID })
	sort.Slice(graph.Contributions, func(i, j int) bool {
		left, right := graph.Contributions[i], graph.Contributions[j]
		if left.Point != right.Point {
			return left.Point < right.Point
		}
		if left.Order != right.Order {
			return left.Order < right.Order
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return left.PluginID < right.PluginID
	})
	return graph, nil
}

func validateExtensionDescriptorSchema(pointID string, raw json.RawMessage) error {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil || schema == nil {
		return fmt.Errorf("extensionPoints %q descriptorSchema 必须是 JSON Schema 对象", pointID)
	}
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		return fmt.Errorf("extensionPoints %q descriptorSchema 必须是闭合 object", pointID)
	}
	if err := rejectRemoteSchemaRefs(schema); err != nil {
		return fmt.Errorf("extensionPoints %q: %w", pointID, err)
	}
	return nil
}

func validateExtensionDescriptor(point ExtensionPointDeclaration, raw json.RawMessage) error {
	compiler := jsonschema.NewCompiler()
	const url = "urn:vastplan:plugin-extension-descriptor"
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(point.DescriptorSchema))
	if err != nil {
		return fmt.Errorf("解析 descriptor Schema: %w", err)
	}
	if err := compiler.AddResource(url, document); err != nil {
		return fmt.Errorf("登记 descriptor Schema: %w", err)
	}
	schema, err := compiler.Compile(url)
	if err != nil {
		return fmt.Errorf("编译 descriptor Schema: %w", err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析 descriptor: %w", err)
	}
	if err := schema.Validate(instance); err != nil {
		return fmt.Errorf("descriptor 不符合扩展点 Schema: %w", err)
	}
	return nil
}

func checkVersionRange(rawConstraint, rawVersion string) error {
	constraint, err := semver.NewConstraint(rawConstraint)
	if err != nil {
		return err
	}
	version, err := semver.NewVersion(rawVersion)
	if err != nil {
		return err
	}
	if !constraint.Check(version) {
		return fmt.Errorf("%s 不满足 %s", rawVersion, rawConstraint)
	}
	return nil
}
