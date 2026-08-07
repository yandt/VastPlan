// Package pluginv1 提供 VastPlan 插件 JSON Schema 的运行时校验入口。
//
// JSON Schema 文件与本包同目录，使 Go 可将它们编译进二进制；文件本身仍是清单、
// 制品元数据和运行时 descriptor 的唯一契约源。其他语言实现必须消费同一批 .json，
// 不得把规则复制成另一套手写类型。
package pluginv1

import (
	_ "embed"
	"encoding/json"
	"maps"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// ManifestSchemaURL 是插件清单 Schema 的稳定标识。
	ManifestSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.plugin.schema.json"
	// DescriptorSchemaURL 是运行态 contribution descriptor Schema 的稳定标识。
	DescriptorSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.descriptor.schema.json"
	// ArtifactSchemaURL 是制品仓库元数据 Schema 的稳定标识。
	ArtifactSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.artifact.schema.json"
	// ArtifactLockSchemaURL 是跨内核精确制品锁 Schema 的稳定标识。
	ArtifactLockSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.artifact-lock.schema.json"
	// ArtifactResolveSchemaURL 是仓库确定性求解输入 Schema 的稳定标识。
	ArtifactResolveSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/plugin/v1/vastplan.artifact-resolve.schema.json"
)

//go:embed vastplan.plugin.schema.json
var manifestSchemaJSON []byte

//go:embed vastplan.descriptor.schema.json
var descriptorSchemaJSON []byte

//go:embed vastplan.artifact.schema.json
var artifactSchemaJSON []byte

//go:embed vastplan.artifact-lock.schema.json
var artifactLockSchemaJSON []byte

//go:embed vastplan.artifact-resolve.schema.json
var artifactResolveSchemaJSON []byte

var (
	compileOnce        sync.Once
	manifestSch        *jsonschema.Schema
	descriptorSch      *jsonschema.Schema
	artifactSch        *jsonschema.Schema
	artifactLockSch    *jsonschema.Schema
	artifactResolveSch *jsonschema.Schema
	compileErr         error
)

// Manifest 是清单中制品服务需要读取的稳定字段。Contributes 保留原始 JSON，
// 因为每个扩展点的详细 descriptor 由 Schema 而非一套会漂移的 Go struct 描述。
type Manifest struct {
	ID                   string                      `json:"id"`
	Name                 string                      `json:"name"`
	Description          string                      `json:"description"`
	Version              string                      `json:"version"`
	Publisher            string                      `json:"publisher"`
	License              string                      `json:"license,omitempty"`
	LicenseFile          string                      `json:"licenseFile,omitempty"`
	NoticeFile           string                      `json:"noticeFile,omitempty"`
	Engines              map[string]string           `json:"engines"`
	Capabilities         *Capabilities               `json:"capabilities,omitempty"`
	ContextAccess        *ContextAccess              `json:"contextAccess,omitempty"`
	Runtime              *RuntimePolicy              `json:"runtime,omitempty"`
	Execution            *ExecutionPolicy            `json:"execution,omitempty"`
	Composition          *CompositionContract        `json:"composition,omitempty"`
	Configuration        *ConfigurationContract      `json:"configuration,omitempty"`
	Authorization        *AuthorizationContract      `json:"authorization,omitempty"`
	State                *State                      `json:"state,omitempty"`
	Activation           []string                    `json:"activation"`
	ExtensionPoints      []ExtensionPointDeclaration `json:"extensionPoints,omitempty"`
	Extensions           []ExtensionContribution     `json:"extensions,omitempty"`
	Dependencies         map[string]string           `json:"dependencies,omitempty"`
	Entry                map[string]string           `json:"entry"`
	FrontendModuleGraphs *FrontendModuleGraphs       `json:"frontendModuleGraphs,omitempty"`
	SupplyChain          *SupplyChain                `json:"supplyChain,omitempty"`
	Contributes          map[string]json.RawMessage  `json:"contributes"`
}

// CompositionContract 声明随 Manifest 签名、可由用户选择的 Feature 开关。
// Feature 只能增加预声明依赖、运行时要求与封闭配置约束，不能携带脚本或
// 用户编写的依赖表达式。
type CompositionContract struct {
	Features []CompositionFeature `json:"features"`
}

type CompositionFeature struct {
	ID                  string               `json:"id"`
	Title               string               `json:"title"`
	Description         string               `json:"description,omitempty"`
	Dependencies        map[string]string    `json:"dependencies,omitempty"`
	RuntimeRequires     []RuntimeRequirement `json:"runtimeRequires,omitempty"`
	ConfigurationSchema json.RawMessage      `json:"configurationSchema,omitempty"`
}

type SupplyChain struct {
	SBOM       *SupplyChainDocument `json:"sbom,omitempty"`
	PythonLock *SupplyChainDocument `json:"pythonLock,omitempty"`
}

type SupplyChainDocument struct {
	Format      string `json:"format"`
	SpecVersion string `json:"specVersion"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
}

// ConfigurationContract declares the plugin-owned configuration surface. The
// JSON Schema covers non-sensitive values. ManagedCredentials are write-only
// form inputs whose values are handed directly to the platform credential
// custodian and never inserted into the schema value document.
type ConfigurationContract struct {
	Scope               string                            `json:"scope"`
	ApplyMode           string                            `json:"applyMode"`
	Schema              json.RawMessage                   `json:"schema"`
	Controller          *ConfigurationController          `json:"controller,omitempty"`
	ResourceController  *ConfigurationResourceController  `json:"resourceController,omitempty"`
	ResourceCollections []ConfigurationResourceCollection `json:"resourceCollections,omitempty"`
	ManagedCredentials  []ManagedCredentialField          `json:"managedCredentials,omitempty"`
}

// ConfigurationController declares that a service-scoped hot configuration
// is owned by the plugin's configuration.v1 control port. The runtime
// capability name is derived from the signed plugin ID and is therefore not a
// second author-maintained identity.
type ConfigurationController struct {
	Protocol string `json:"protocol"`
}

// ConfigurationResourceController declares a plugin-owned collection of
// independently versioned configuration resources. The first published kind
// is profile; the wire remains generic so future bounded resource kinds do not
// need a domain-specific lifecycle API.
type ConfigurationResourceController struct {
	Protocol string `json:"protocol"`
}

type ConfigurationResourceCollection struct {
	ID                 string                   `json:"id"`
	Kind               string                   `json:"kind"`
	Title              string                   `json:"title"`
	Description        string                   `json:"description,omitempty"`
	Schema             json.RawMessage          `json:"schema"`
	ManagedCredentials []ManagedCredentialField `json:"managedCredentials,omitempty"`
	MinItems           uint32                   `json:"minItems,omitempty"`
	MaxItems           uint32                   `json:"maxItems"`
}

type ManagedCredentialField struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Purpose     string `json:"purpose"`
	Required    bool   `json:"required,omitempty"`
}

// ContextAccess declares the semantic CallContext views requested by a signed
// plugin manifest. It is only a request; host, publisher and boundary ceilings
// can remove optional fields or reject unavailable required fields.
type ContextAccess struct {
	Required []string `json:"required,omitempty"`
	Optional []string `json:"optional,omitempty"`
	Baggage  []string `json:"baggage,omitempty"`
}

func ContextAccessContract(manifest Manifest) ContextAccess {
	if manifest.ContextAccess == nil {
		return ContextAccess{}
	}
	return ContextAccess{
		Required: append([]string(nil), manifest.ContextAccess.Required...),
		Optional: append([]string(nil), manifest.ContextAccess.Optional...),
		Baggage:  append([]string(nil), manifest.ContextAccess.Baggage...),
	}
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
	Node             *NodeExecution      `json:"node,omitempty"`
	Python           *PythonExecution    `json:"python,omitempty"`
	DynamicGo        *DynamicGoExecution `json:"dynamicGo,omitempty"`
}

// NodeExecution 是 Node Worker 执行单元的显式兼容声明。WorkerSafe 必须为
// true，入口必须使用 ESM；缺少声明不能被驱动推断为兼容。
type NodeExecution struct {
	WorkerSafe   bool   `json:"workerSafe"`
	ModuleFormat string `json:"moduleFormat"`
}

// PythonExecution 是插件作者对其完整依赖图的多解释器安全承诺。宿主仍会探测
// CPython 版本和 Runtime Host 能力，清单声明不能绕过运行时校验。
type PythonExecution struct {
	SubinterpreterSafe bool `json:"subinterpreterSafe"`
}

// DynamicGoExecution 声明制品内可选的首方 Go 动态内嵌入口。它只描述已签名内容，
// 是否允许加载仍由节点 PlacementPolicy 决定；Required 只能禁止进程回退，不能授予内嵌权限。
type DynamicGoExecution struct {
	Entry       string `json:"entry"`
	ABI         string `json:"abi"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Required    bool   `json:"required,omitempty"`
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
	if execution.Node != nil {
		node := *execution.Node
		execution.Node = &node
	}
	if execution.Python != nil {
		python := *execution.Python
		execution.Python = &python
	}
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
	InstancePolicy    string                    `json:"instancePolicy"`
	StateModel        string                    `json:"stateModel"`
	Visibility        string                    `json:"visibility"`
	Routing           string                    `json:"routing"`
	RoutingDomain     string                    `json:"routingDomain,omitempty"`
	BackgroundService bool                      `json:"backgroundService,omitempty"`
	Provides          []RuntimeCapabilityPolicy `json:"provides,omitempty"`
	Requires          []RuntimeRequirement      `json:"requires,omitempty"`
}

type RuntimeCapabilityPolicy struct {
	ExtensionPoint  string `json:"extensionPoint"`
	Capability      string `json:"capability"`
	ContractVersion string `json:"contractVersion"`
	Visibility      string `json:"visibility,omitempty"`
	Routing         string `json:"routing,omitempty"`
	RoutingDomain   string `json:"routingDomain,omitempty"`
}

// RuntimeRequirement 描述跨插件/跨服务的运行时能力依赖，不与制品 dependencies 混用。
type RuntimeRequirement struct {
	Capability     string `json:"capability"`
	ContractRange  string `json:"contractRange"`
	Scope          string `json:"scope"`
	Kind           string `json:"kind"`
	Ready          string `json:"ready"`
	FailurePolicy  string `json:"failurePolicy"`
	LogicalService string `json:"logicalService,omitempty"`
	RoutingDomain  string `json:"routingDomain,omitempty"`
}

// DataModelReference binds a signed plugin manifest to one external
// data.model.v1 document without inflating the manifest itself.
type DataModelReference struct {
	ID              string `json:"id"`
	ContractVersion string `json:"contractVersion"`
	Path            string `json:"path"`
	SHA256          string `json:"sha256"`
}

// DataMigrationReference binds a migration document to the same signed plugin
// artifact as its target DataModel. A digest match without an artifact trust
// decision is insufficient for execution.
type DataMigrationReference struct {
	ID              string `json:"id"`
	ContractVersion string `json:"contractVersion"`
	ModelID         string `json:"modelId"`
	FromVersion     uint64 `json:"fromVersion"`
	ToVersion       uint64 `json:"toVersion"`
	Path            string `json:"path"`
	SHA256          string `json:"sha256"`
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

// Capabilities 声明插件运行需求。KernelServices 既参与装配，也作为签名申请进入
// 可信宿主的 Grant 编译；清单申请本身不等于授权。
type Capabilities struct {
	KernelServices []string `json:"kernelServices,omitempty"`
	Credentials    []string `json:"credentials,omitempty"`
	Resources      []string `json:"resources,omitempty"`
}

// RuntimeContribution 是签名清单对运行时声明的授权边界。运行进程只能声明这里
// 已登记的扩展点、ID、优先级和 descriptor，不能在启动后临时扩大权限面。
type RuntimeContribution struct {
	ExtensionPoint  string          `json:"extensionPoint"`
	ID              string          `json:"id"`
	ContractVersion string          `json:"contractVersion,omitempty"`
	Priority        int32           `json:"priority"`
	Descriptor      json.RawMessage `json:"descriptor"`
	InstancePolicy  string          `json:"instancePolicy,omitempty"`
	StateModel      string          `json:"stateModel,omitempty"`
	Visibility      string          `json:"visibility,omitempty"`
	Routing         string          `json:"routing,omitempty"`
	RoutingDomain   string          `json:"routingDomain,omitempty"`
}

var backendContributionPoints = map[string]string{
	"tools":                        "tool.package",
	"agents":                       "agent",
	"apiRoutes":                    "api.route",
	"permissionCheckers":           "permission.checker",
	"eventSinks":                   "event.sink",
	"hooks":                        "hook",
	"desktopCapabilities":          "desktop.capability",
	"authenticationProviders":      "authentication.provider",
	"configurationScopedResolvers": ConfigurationScopedResolverExtensionPoint,
}

var declarativeBackendContributionGroups = map[string]struct{}{
	"apiContracts":      {},
	"dataPlaneServices": {},
	"dataModels":        {},
	"dataMigrations":    {},
}

// BackendRuntimeContributions 把已经通过 Schema 的 backend 清单贡献规范化为协议总线
// 可比较的声明。id/priority 属于注册元数据，其余字段构成运行态 descriptor。
