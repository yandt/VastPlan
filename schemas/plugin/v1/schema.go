// Package pluginv1 提供 VastPlan 插件 JSON Schema 的运行时校验入口。
//
// JSON Schema 文件与本包同目录，使 Go 可将它们编译进二进制；文件本身仍是清单、
// 制品元数据和运行时 descriptor 的唯一契约源。其他语言实现必须消费同一批 .json，
// 不得把规则复制成另一套手写类型。
package pluginv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"sync"

	commonv1 "cdsoft.com.cn/VastPlan/schemas/common/v1"
	"cdsoft.com.cn/VastPlan/shared/go/pluginid"
	"cdsoft.com.cn/VastPlan/shared/go/servicemodel"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// ManifestSchemaURL 是插件清单 Schema 的稳定标识。
	ManifestSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.plugin.schema.json"
	// DescriptorSchemaURL 是运行态 contribution descriptor Schema 的稳定标识。
	DescriptorSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.descriptor.schema.json"
	// ArtifactSchemaURL 是制品仓库元数据 Schema 的稳定标识。
	ArtifactSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.artifact.schema.json"
)

//go:embed vastplan.plugin.schema.json
var manifestSchemaJSON []byte

//go:embed vastplan.descriptor.schema.json
var descriptorSchemaJSON []byte

//go:embed vastplan.artifact.schema.json
var artifactSchemaJSON []byte

var (
	compileOnce   sync.Once
	manifestSch   *jsonschema.Schema
	descriptorSch *jsonschema.Schema
	artifactSch   *jsonschema.Schema
	compileErr    error
)

// Manifest 是清单中制品服务需要读取的稳定字段。Contributes 保留原始 JSON，
// 因为每个扩展点的详细 descriptor 由 Schema 而非一套会漂移的 Go struct 描述。
type Manifest struct {
	ID           string                     `json:"id"`
	Name         string                     `json:"name"`
	Description  string                     `json:"description"`
	Version      string                     `json:"version"`
	Publisher    string                     `json:"publisher"`
	License      string                     `json:"license,omitempty"`
	LicenseFile  string                     `json:"licenseFile,omitempty"`
	NoticeFile   string                     `json:"noticeFile,omitempty"`
	Engines      map[string]string          `json:"engines"`
	Capabilities *Capabilities              `json:"capabilities,omitempty"`
	Runtime      *RuntimePolicy             `json:"runtime,omitempty"`
	Execution    *ExecutionPolicy           `json:"execution,omitempty"`
	State        *State                     `json:"state,omitempty"`
	Activation   []string                   `json:"activation"`
	Dependencies map[string]string          `json:"dependencies,omitempty"`
	Entry        map[string]string          `json:"entry"`
	Contributes  map[string]json.RawMessage `json:"contributes"`
}

// ExecutionPolicy 描述各运行面的启动方式。它只声明驱动与最低要求；发布者信任级别
// 和最终隔离强度由节点策略决定，插件不能通过自报把自己提升为第一方。
type ExecutionPolicy struct {
	Backend *BackendExecution `json:"backend,omitempty"`
}

// BackendExecution 是语言无关的 Backend 启动契约。Driver 是可扩展标识，不把内核
// 锁死在当前 native/python 实现；未来 OCI/WASM 驱动沿用同一结构。
type BackendExecution struct {
	Driver           string              `json:"driver"`
	Args             []string            `json:"args,omitempty"`
	Requirements     map[string]string   `json:"requirements,omitempty"`
	Platforms        []string            `json:"platforms,omitempty"`
	MinimumIsolation string              `json:"minimumIsolation,omitempty"`
	Features         []string            `json:"features,omitempty"`
	DynamicGo        *DynamicGoExecution `json:"dynamicGo,omitempty"`
}

// DynamicGoExecution 声明制品内可选的首方 Go 动态内嵌入口。它只描述已签名内容，
// 是否允许加载仍由节点 PlacementPolicy 决定。
type DynamicGoExecution struct {
	Entry       string `json:"entry"`
	ABI         string `json:"abi"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// BackendExecutionContract 返回向后兼容的冻结执行契约。旧 v1 清单等价于 native
// trusted-process，仍从 entry.backend 启动。
func BackendExecutionContract(manifest Manifest) BackendExecution {
	if manifest.Execution == nil || manifest.Execution.Backend == nil {
		return BackendExecution{Driver: "native", MinimumIsolation: "trusted-process"}
	}
	execution := *manifest.Execution.Backend
	execution.Args = append([]string(nil), execution.Args...)
	execution.Platforms = append([]string(nil), execution.Platforms...)
	execution.Features = append([]string(nil), execution.Features...)
	if execution.DynamicGo != nil {
		dynamic := *execution.DynamicGo
		execution.DynamicGo = &dynamic
	}
	if execution.MinimumIsolation == "" {
		execution.MinimumIsolation = "trusted-process"
	}
	if execution.Requirements != nil {
		execution.Requirements = maps.Clone(execution.Requirements)
	}
	return execution
}

// RuntimePolicy 声明插件贡献的实例化策略和默认能力边界。
// Provides 可按 extensionPoint + capability 覆盖顶层策略。
type RuntimePolicy struct {
	InstancePolicy string                    `json:"instancePolicy"`
	StateModel     string                    `json:"stateModel"`
	Visibility     string                    `json:"visibility"`
	Routing        string                    `json:"routing"`
	RoutingDomain  string                    `json:"routingDomain,omitempty"`
	Provides       []RuntimeCapabilityPolicy `json:"provides,omitempty"`
	Requires       []RuntimeRequirement      `json:"requires,omitempty"`
}

type RuntimeCapabilityPolicy struct {
	ExtensionPoint string `json:"extensionPoint"`
	Capability     string `json:"capability"`
	Visibility     string `json:"visibility,omitempty"`
	Routing        string `json:"routing,omitempty"`
	RoutingDomain  string `json:"routingDomain,omitempty"`
}

// RuntimeRequirement 描述跨插件/跨服务的运行时能力依赖，不与制品 dependencies 混用。
type RuntimeRequirement struct {
	Capability     string `json:"capability"`
	Version        string `json:"version,omitempty"`
	Scope          string `json:"scope"`
	Kind           string `json:"kind"`
	Ready          string `json:"ready"`
	FailurePolicy  string `json:"failurePolicy"`
	LogicalService string `json:"logicalService,omitempty"`
	RoutingDomain  string `json:"routingDomain,omitempty"`
}

// State 声明各运行面的插件私有持久状态。Backend 1.0 只发布 backend 契约；
// 其他运行面在各自内核封板时追加，不能借 additionalProperties 提前占位。
type State struct {
	Backend *BackendState `json:"backend,omitempty"`
}

// StateIdentity 是一个不可猜测的插件私有状态格式。FormatVersion 只在同一 Format
// 内递增；跨 Format 迁移也必须在 Migration.From 中逐项声明。
type StateIdentity struct {
	Format        string `json:"format"`
	FormatVersion int32  `json:"formatVersion"`
}

// MigrationRequest 是插件迁移处理器接收的稳定事务负载；阶段由生命周期操作单独表达。
type MigrationRequest struct {
	TransactionID string        `json:"transactionId"`
	From          StateIdentity `json:"from"`
	To            StateIdentity `json:"to"`
}

// BackendState 声明当前格式，以及新版本可通过 lifecycle.v1 从哪些旧格式迁移。
// 首次引入持久状态时 Migration 可省略；一旦升级改变格式，Reconciler 会强制要求。
type BackendState struct {
	StateIdentity
	Migration *StateMigration `json:"migration,omitempty"`
}

type StateMigration struct {
	Protocol string          `json:"protocol"`
	From     []StateIdentity `json:"from"`
}

// Capabilities 是装配元数据，不承担运行时权限强制职责。
type Capabilities struct {
	KernelServices []string `json:"kernelServices,omitempty"`
	Credentials    []string `json:"credentials,omitempty"`
	Resources      []string `json:"resources,omitempty"`
}

// RuntimeContribution 是签名清单对运行时声明的授权边界。运行进程只能声明这里
// 已登记的扩展点、ID、优先级和 descriptor，不能在启动后临时扩大权限面。
type RuntimeContribution struct {
	ExtensionPoint string          `json:"extensionPoint"`
	ID             string          `json:"id"`
	Priority       int32           `json:"priority"`
	Descriptor     json.RawMessage `json:"descriptor"`
	InstancePolicy string          `json:"instancePolicy,omitempty"`
	StateModel     string          `json:"stateModel,omitempty"`
	Visibility     string          `json:"visibility,omitempty"`
	Routing        string          `json:"routing,omitempty"`
	RoutingDomain  string          `json:"routingDomain,omitempty"`
}

var backendContributionPoints = map[string]string{
	"tools":              "tool.package",
	"agents":             "agent",
	"apiRoutes":          "api.route",
	"permissionCheckers": "permission.checker",
	"eventSinks":         "event.sink",
	"hooks":              "hook",
	"runnerCapabilities": "runner.capability",
}

// BackendRuntimeContributions 把已经通过 Schema 的 backend 清单贡献规范化为协议总线
// 可比较的声明。id/priority 属于注册元数据，其余字段构成运行态 descriptor。
func BackendRuntimeContributions(manifest Manifest) ([]RuntimeContribution, error) {
	raw := manifest.Contributes["backend"]
	if len(raw) == 0 {
		return nil, nil
	}
	var groups map[string][]map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&groups); err != nil {
		return nil, fmt.Errorf("解析 backend contributions: %w", err)
	}
	var out []RuntimeContribution
	defaultPolicy, overrides, err := runtimePolicies(manifest)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for group, entries := range groups {
		point, ok := backendContributionPoints[group]
		if !ok {
			return nil, fmt.Errorf("未知 backend contribution 组 %q", group)
		}
		for _, entry := range entries {
			id, _ := entry["id"].(string)
			if id == "" {
				return nil, fmt.Errorf("%s contribution 缺少 id", group)
			}
			priority := int32(0)
			if number, ok := entry["priority"].(json.Number); ok {
				parsed, err := number.Int64()
				if err != nil {
					return nil, fmt.Errorf("%s/%s priority 非整数: %w", point, id, err)
				}
				priority = int32(parsed)
			}
			delete(entry, "id")
			delete(entry, "priority")
			delete(entry, "service_role") // 装配归属由签名清单和 RuntimeUnit 单独强制。
			descriptor, err := json.Marshal(entry)
			if err != nil {
				return nil, fmt.Errorf("规范化 %s/%s descriptor: %w", point, id, err)
			}
			if err := ValidateDescriptor(point, descriptor); err != nil {
				return nil, err
			}
			policy := defaultPolicy
			if override, ok := overrides[point+"\x00"+id]; ok {
				policy.Visibility = override.Visibility
				policy.Routing = override.Routing
				policy.RoutingDomain = override.RoutingDomain
				policy = servicemodel.Normalize(policy)
			}
			key := point + "\x00" + id
			if _, duplicate := seen[key]; duplicate {
				return nil, fmt.Errorf("运行时贡献重复: %s/%s", point, id)
			}
			seen[key] = struct{}{}
			out = append(out, RuntimeContribution{
				ExtensionPoint: point, ID: id, Priority: priority, Descriptor: descriptor,
				InstancePolicy: policy.InstancePolicy, StateModel: policy.StateModel,
				Visibility: policy.Visibility, Routing: policy.Routing, RoutingDomain: policy.RoutingDomain,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ExtensionPoint != out[j].ExtensionPoint {
			return out[i].ExtensionPoint < out[j].ExtensionPoint
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func runtimePolicies(manifest Manifest) (servicemodel.Policy, map[string]RuntimeCapabilityPolicy, error) {
	if manifest.Runtime == nil {
		return servicemodel.Normalize(servicemodel.Policy{}), nil, nil
	}
	policy := servicemodel.Policy{
		InstancePolicy: manifest.Runtime.InstancePolicy,
		StateModel:     manifest.Runtime.StateModel,
		Visibility:     manifest.Runtime.Visibility,
		Routing:        manifest.Runtime.Routing,
		RoutingDomain:  manifest.Runtime.RoutingDomain,
	}
	policy = servicemodel.Normalize(policy)
	if err := servicemodel.Validate(policy); err != nil {
		return servicemodel.Policy{}, nil, fmt.Errorf("runtime 策略无效: %w", err)
	}
	overrides := make(map[string]RuntimeCapabilityPolicy, len(manifest.Runtime.Provides))
	for _, provide := range manifest.Runtime.Provides {
		if provide.ExtensionPoint == "" || provide.Capability == "" {
			return servicemodel.Policy{}, nil, fmt.Errorf("runtime.provides 必须填写 extensionPoint 和 capability")
		}
		key := provide.ExtensionPoint + "\x00" + provide.Capability
		if _, exists := overrides[key]; exists {
			return servicemodel.Policy{}, nil, fmt.Errorf("runtime.provides 重复: %s/%s", provide.ExtensionPoint, provide.Capability)
		}
		override := provide
		overridePolicy := policy
		overridePolicy.Visibility = provide.Visibility
		overridePolicy.Routing = provide.Routing
		overridePolicy.RoutingDomain = provide.RoutingDomain
		overridePolicy = servicemodel.Normalize(overridePolicy)
		if err := servicemodel.Validate(overridePolicy); err != nil {
			return servicemodel.Policy{}, nil, fmt.Errorf("runtime.provides %s/%s 策略无效: %w", provide.ExtensionPoint, provide.Capability, err)
		}
		override.Visibility = overridePolicy.Visibility
		override.Routing = overridePolicy.Routing
		override.RoutingDomain = overridePolicy.RoutingDomain
		overrides[key] = override
	}
	if err := validateRuntimeRequirements(manifest.Runtime.Requires); err != nil {
		return servicemodel.Policy{}, nil, err
	}
	return policy, overrides, nil
}

func validateRuntimeRequirements(requirements []RuntimeRequirement) error {
	seen := make(map[string]struct{}, len(requirements))
	for _, requirement := range requirements {
		if requirement.Capability == "" {
			return fmt.Errorf("runtime.requires capability 不能为空")
		}
		if requirement.Scope != "same-node" && requirement.Scope != "same-kernel" && requirement.Scope != "remote" {
			return fmt.Errorf("runtime.requires %s scope 无效: %q", requirement.Capability, requirement.Scope)
		}
		if requirement.Kind != "strong" && requirement.Kind != "soft" && requirement.Kind != "lazy" && requirement.Kind != "data" {
			return fmt.Errorf("runtime.requires %s kind 无效: %q", requirement.Capability, requirement.Kind)
		}
		if requirement.Ready != "readiness" && requirement.Ready != "health" {
			return fmt.Errorf("runtime.requires %s ready 无效: %q", requirement.Capability, requirement.Ready)
		}
		if requirement.FailurePolicy != "fail" && requirement.FailurePolicy != "degrade" && requirement.FailurePolicy != "retry" {
			return fmt.Errorf("runtime.requires %s failurePolicy 无效: %q", requirement.Capability, requirement.FailurePolicy)
		}
		key := requirement.Capability + "\x00" + requirement.LogicalService + "\x00" + requirement.RoutingDomain
		if _, exists := seen[key]; exists {
			return fmt.Errorf("runtime.requires 重复: %s", requirement.Capability)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func schemas() error {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := commonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		for url, raw := range map[string][]byte{
			ManifestSchemaURL:   manifestSchemaJSON,
			DescriptorSchemaURL: descriptorSchemaJSON,
			ArtifactSchemaURL:   artifactSchemaJSON,
		} {
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				compileErr = fmt.Errorf("解析内置 Schema %s: %w", url, err)
				return
			}
			if err := compiler.AddResource(url, doc); err != nil {
				compileErr = fmt.Errorf("登记内置 Schema %s: %w", url, err)
				return
			}
		}
		manifestSch, compileErr = compiler.Compile(ManifestSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译插件清单 Schema: %w", compileErr)
			return
		}
		descriptorSch, compileErr = compiler.Compile(DescriptorSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译 descriptor Schema: %w", compileErr)
			return
		}
		artifactSch, compileErr = compiler.Compile(ArtifactSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译制品元数据 Schema: %w", compileErr)
		}
	})
	return compileErr
}

// ParseManifest 校验并解析清单。任何未知字段、缺失必填字段或不合法 descriptor
// 都在制品进入仓库前被拒绝，调用方不可绕过 Schema 直接反序列化。
func ParseManifest(raw []byte) (Manifest, error) {
	if err := schemas(); err != nil {
		return Manifest{}, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Manifest{}, fmt.Errorf("解析插件清单 JSON: %w", err)
	}
	if err := manifestSch.Validate(instance); err != nil {
		return Manifest{}, fmt.Errorf("插件清单不符合 Schema: %w", err)
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("解析插件清单字段: %w", err)
	}
	if err := pluginid.ValidatePublisherOwnership(manifest.ID, manifest.Publisher); err != nil {
		return Manifest{}, err
	}
	if manifest.Runtime != nil {
		if _, _, err := runtimePolicies(manifest); err != nil {
			return Manifest{}, err
		}
	}
	return manifest, nil
}

// ValidateDescriptor 校验插件通过协议总线注册的一条运行态 descriptor。
// 它把 extension point 和 descriptor 一起送入 Schema，避免只校验 JSON 语法而放过
// 诸如 hook phase 拼错这类会让分发语义失真的错误。
func ValidateDescriptor(extensionPoint string, raw []byte) error {
	if err := schemas(); err != nil {
		return err
	}
	var descriptor any
	if err := json.Unmarshal(raw, &descriptor); err != nil {
		return fmt.Errorf("解析 %s descriptor JSON: %w", extensionPoint, err)
	}
	instance := map[string]any{"extensionPoint": extensionPoint, "descriptor": descriptor}
	if err := descriptorSch.Validate(instance); err != nil {
		return fmt.Errorf("%s descriptor 不符合 Schema: %w", extensionPoint, err)
	}
	return nil
}

// ValidateArtifactMetadata 校验制品索引 JSON；仓库发布和读取都调用它，避免索引
// 被手工写坏后仍被下游 reconcile 采用。
func ValidateArtifactMetadata(raw []byte) error {
	if err := schemas(); err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("解析制品元数据 JSON: %w", err)
	}
	if err := artifactSch.Validate(instance); err != nil {
		return fmt.Errorf("制品元数据不符合 Schema: %w", err)
	}
	return nil
}
