package backendcompositionv1

import (
	"encoding/json"
	"sort"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const (
	ApplicationIntentSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/composition/backend/v1/vastplan.application-intent.schema.json"
	ResolutionReportSchemaURL  = "https://schemas.cdsoft.com.cn/vastplan/composition/backend/v1/vastplan.resolution-report.schema.json"
	PlanningCapability         = "platform.composition.plan"
	PlanningOperation          = "plan"

	ResolutionResolved           = "Resolved"
	ResolutionNeedsConfiguration = "NeedsConfiguration"
	ResolutionInvalid            = "Invalid"
)

// ApplicationIntent 是应用管理员唯一可写的 Backend 服务组合文档。
// 运行拓扑仍由 Planner 编译进 ApplicationComposition。
type ApplicationIntent struct {
	compositioncommonv1.Document
	Target   compositioncommonv1.Target `json:"target"`
	Metadata deploymentv1.Metadata      `json:"metadata"`
	Services []ServiceIntent            `json:"services"`
}

type ServiceIntent struct {
	ID           string                         `json:"id"`
	ServiceClass string                         `json:"serviceClass"`
	RootPlugins  []pluginv1.ArtifactRequirement `json:"rootPlugins"`
	PluginConfig map[string]map[string]any      `json:"pluginConfig,omitempty"`
	Operations   ServiceOperationsIntent        `json:"operations"`
}

// ServiceOperationsIntent 有意窄于 deployment/v2 的 ServiceUnit，只包含可独立授权
// 的容量与放置输入；策略、路由和依赖绝不接受用户直接输入。
type ServiceOperationsIntent struct {
	Replicas    int                                `json:"replicas"`
	Autoscaling *deploymentv2.Autoscaling          `json:"autoscaling,omitempty"`
	Resources   *deploymentv2.ResourceRequirements `json:"resources,omitempty"`
	Placement   *deploymentv2.Placement            `json:"placement,omitempty"`
}

type PlannerIdentity struct {
	Ref                 pluginv1.ArtifactRef `json:"ref"`
	Capability          string               `json:"capability"`
	ConfigurationDigest string               `json:"configurationDigest"`
}

// PlanningRequest 是 Deployment Manager 调用 Planner 时唯一允许的输入。
// 仓库策略和 Planner 身份由插件自身的受信配置注入，不能由调用方覆盖。
type PlanningRequest struct {
	Intent                ApplicationIntent              `json:"intent"`
	PlatformProfile       PlatformProfile                `json:"platformProfile"`
	ConfigurationSnapshot *PlanningConfigurationSnapshot `json:"configurationSnapshot,omitempty"`
}

// PlanningConfigurationSnapshot 只能由可信配置 Provider 生成。
// 它只携带不透明 CredentialRef，不包含 material，也不进入用户可写 Intent。
type PlanningConfigurationSnapshot struct {
	Version  int                         `json:"version"`
	Bindings []PlanningCredentialBinding `json:"bindings"`
	Digest   string                      `json:"digest"`
}

type PlanningCredentialBinding struct {
	UnitID   string                        `json:"unitId"`
	PluginID string                        `json:"pluginId"`
	FieldID  string                        `json:"fieldId"`
	Ref      commonv1.ManagedCredentialRef `json:"ref"`
}

func (s PlanningConfigurationSnapshot) ComputedDigest() string {
	s.Digest = ""
	s.Bindings = append([]PlanningCredentialBinding(nil), s.Bindings...)
	sort.Slice(s.Bindings, func(i, j int) bool {
		if s.Bindings[i].UnitID != s.Bindings[j].UnitID {
			return s.Bindings[i].UnitID < s.Bindings[j].UnitID
		}
		if s.Bindings[i].PluginID != s.Bindings[j].PluginID {
			return s.Bindings[i].PluginID < s.Bindings[j].PluginID
		}
		return s.Bindings[i].FieldID < s.Bindings[j].FieldID
	})
	return compositioncommonv1.Digest(s)
}

type ResolvedFeature struct {
	UnitID    string `json:"unitId"`
	PluginID  string `json:"pluginId"`
	FeatureID string `json:"featureId"`
}

type CapabilityProviderBinding struct {
	ConsumerUnitID   string `json:"consumerUnitId"`
	Capability       string `json:"capability"`
	ProviderUnitID   string `json:"providerUnitId"`
	ProviderPluginID string `json:"providerPluginId"`
	ContractVersion  string `json:"contractVersion"`
	LogicalService   string `json:"logicalService,omitempty"`
	RoutingDomain    string `json:"routingDomain,omitempty"`
}

type ServiceDependencyGraph struct {
	Nodes []ServiceDependencyNode `json:"nodes"`
	Edges []ServiceDependencyEdge `json:"edges"`
}

type ServiceDependencyNode struct {
	UnitID       string `json:"unitId"`
	ServiceClass string `json:"serviceClass"`
}

type ServiceDependencyEdge struct {
	FromUnitID    string `json:"fromUnitId"`
	ToUnitID      string `json:"toUnitId"`
	Capability    string `json:"capability"`
	Kind          string `json:"kind"`
	FailurePolicy string `json:"failurePolicy"`
}

type ConfigurationPlan struct {
	Items  []ConfigurationPlanItem `json:"items"`
	Digest string                  `json:"digest"`
}

type ConfigurationPlanItem struct {
	UnitID              string                     `json:"unitId"`
	PluginID            string                     `json:"pluginId"`
	Source              string                     `json:"source"`
	Editable            bool                       `json:"editable"`
	SchemaDigest        string                     `json:"schemaDigest"`
	ConfigurationDigest string                     `json:"configurationDigest"`
	DependencyPath      []string                   `json:"dependencyPath"`
	Missing             []ConfigurationRequirement `json:"missing,omitempty"`
}

type ConfigurationRequirement struct {
	Kind  string `json:"kind"`
	Field string `json:"field"`
}

type ResolutionDiagnostic struct {
	Severity string   `json:"severity"`
	Code     string   `json:"code"`
	Path     []string `json:"path,omitempty"`
	Message  string   `json:"message"`
}

// ResolutionReport 绑定复现或拒绝编译方案所需的全部输入与解释，但绝不携带凭证材料。
type ResolutionReport struct {
	Version                      int                         `json:"version"`
	Intent                       compositioncommonv1.Ref     `json:"intent"`
	PlatformProfile              compositioncommonv1.Ref     `json:"platformProfile"`
	Planner                      PlannerIdentity             `json:"planner"`
	Status                       string                      `json:"status"`
	ApplicationComposition       *ApplicationComposition     `json:"applicationComposition,omitempty"`
	ApplicationCompositionDigest string                      `json:"applicationCompositionDigest,omitempty"`
	ArtifactLock                 *pluginv1.ArtifactLock      `json:"artifactLock,omitempty"`
	Features                     []ResolvedFeature           `json:"features"`
	ProviderBindings             []CapabilityProviderBinding `json:"providerBindings"`
	ServiceGraph                 ServiceDependencyGraph      `json:"serviceGraph"`
	ConfigurationPlan            ConfigurationPlan           `json:"configurationPlan"`
	Diagnostics                  []ResolutionDiagnostic      `json:"diagnostics"`
	PlanDigest                   string                      `json:"planDigest"`
}

func (i ApplicationIntent) Digest() string { return compositioncommonv1.Digest(i) }

func (p ConfigurationPlan) ComputedDigest() string {
	p.Digest = ""
	return compositioncommonv1.Digest(p)
}

func (r ResolutionReport) ComputedPlanDigest() string {
	r.PlanDigest = ""
	return compositioncommonv1.Digest(r)
}

// FinalizeResolutionReport 先规范化所有确定性集合并绑定嵌套摘要，随后执行与
// Wire 入口相同的校验。
func FinalizeResolutionReport(report ResolutionReport) (ResolutionReport, error) {
	if report.ApplicationComposition != nil {
		composition, err := ValidateApplicationComposition(*report.ApplicationComposition)
		if err != nil {
			return ResolutionReport{}, err
		}
		report.ApplicationComposition = &composition
		report.ApplicationCompositionDigest = composition.Digest()
	}
	report.ConfigurationPlan.Digest = ""
	report.PlanDigest = ""
	normalized, err := NormalizeResolutionReport(report)
	if err != nil {
		return ResolutionReport{}, err
	}
	normalized.ConfigurationPlan.Digest = normalized.ConfigurationPlan.ComputedDigest()
	normalized.PlanDigest = normalized.ComputedPlanDigest()
	return ValidateResolutionReport(normalized)
}

// MarshalNormalized 返回 Schema 与语义规范化后供 SDK 一致性测试使用的编码。
func (r ResolutionReport) MarshalNormalized() ([]byte, error) { return json.Marshal(r) }
