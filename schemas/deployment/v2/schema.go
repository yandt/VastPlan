// Package deploymentv2 定义控制面多节点 service 部署规格。
//
// v2 是 Composition Resolver 生成的全局调度锁；调度器把它展开成每节点一份
// deployment/v1 执行快照，Node Agent 因而不承担集群仲裁，也不会因为多个节点
// 各自计算而重复启动同一副本。
package deploymentv2

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	commonv1 "cdsoft.com.cn/VastPlan/schemas/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	"cdsoft.com.cn/VastPlan/shared/go/servicemodel"
)

const DeploymentSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/deployment/v2/vastplan.deployment.schema.json"

//go:embed vastplan.deployment.schema.json
var deploymentSchemaJSON []byte

var (
	compileOnce      sync.Once
	deploymentSchema *jsonschema.Schema
	compileErr       error
)

type Deployment struct {
	Version     int                   `json:"version"`
	Revision    uint64                `json:"revision"`
	Metadata    deploymentv1.Metadata `json:"metadata"`
	Resolution  Resolution            `json:"resolution"`
	Units       []ServiceUnit         `json:"units"`
	AppProfiles []AppProfileRef       `json:"app_profiles,omitempty"`
}

const (
	OriginPlatformProfile = "platform-profile"
	OriginApplication     = "application"
)

// Resolution 锁定生成 Deployment 的两份配置输入及每个插件的管理来源。
// Controller 依赖该信息再次执行服务端授权校验，不能接受缺失来源的插件。
type Resolution struct {
	PlatformProfile        CompositionRef    `json:"platform_profile"`
	ApplicationComposition CompositionRef    `json:"application_composition"`
	DevelopmentMode        bool              `json:"development_mode"`
	PluginOrigins          map[string]string `json:"plugin_origins"`
}

type CompositionRef struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
	Digest   string `json:"digest"`
}

// AppProfileRef pins an independently built App Profile artifact. It is part
// of deployment intent, but is not a ServiceUnit and is never scheduled by the
// backend service scheduler.
type AppProfileRef struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
	Digest   string `json:"digest"`
}

type ServiceUnit struct {
	ID             string                   `json:"id"`
	Kind           string                   `json:"kind"`
	Plugins        []deploymentv1.PluginRef `json:"plugins"`
	Config         map[string]any           `json:"config,omitempty"`
	Enabled        bool                     `json:"enabled"`
	ServiceRole    string                   `json:"service_role"`
	LogicalService string                   `json:"logical_service,omitempty"`
	InstancePolicy string                   `json:"instance_policy,omitempty"`
	StateModel     string                   `json:"state_model,omitempty"`
	Visibility     string                   `json:"visibility,omitempty"`
	Routing        string                   `json:"routing,omitempty"`
	RoutingDomain  string                   `json:"routing_domain,omitempty"`
	PartitionKeys  []string                 `json:"partition_keys,omitempty"`
	DependsOn      []string                 `json:"depends_on,omitempty"`
	Replicas       int                      `json:"replicas"`
	Autoscaling    *Autoscaling             `json:"autoscaling,omitempty"`
	Resources      ResourceRequirements     `json:"resources,omitempty"`
	Placement      Placement                `json:"placement,omitempty"`
}

// ResourceList 和 ResourceRequirements 是 common/v1 稳定 DTO 的兼容别名。
type ResourceList = commonv1.ResourceList
type ResourceRequirements = commonv1.ResourceRequirements

type LabelTerm struct {
	MatchLabels map[string]string `json:"match_labels"`
}

type WeightedLabelTerm struct {
	MatchLabels map[string]string `json:"match_labels"`
	Weight      int               `json:"weight"`
}

type LabelPolicy struct {
	Required  []LabelTerm         `json:"required,omitempty"`
	Preferred []WeightedLabelTerm `json:"preferred,omitempty"`
}

type Placement struct {
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	Affinity     LabelPolicy       `json:"affinity,omitempty"`
	AntiAffinity LabelPolicy       `json:"antiAffinity,omitempty"`
}

type Autoscaling struct {
	MinReplicas           int     `json:"min_replicas"`
	MaxReplicas           int     `json:"max_replicas"`
	Metric                string  `json:"metric"`
	TargetValuePerReplica float64 `json:"target_value_per_replica"`
}

func schema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		deploymentSchema, compileErr = compiler.Compile(DeploymentSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译集群部署 Schema: %w", compileErr)
		}
	})
	return deploymentSchema, compileErr
}

// AddResources 向外部编译器登记 Deployment v2 及其公共依赖，供组合 Schema
// 直接引用同一份 ServiceUnit 定义，避免复制调度 DTO。
func AddResources(compiler *jsonschema.Compiler) error {
	if err := commonv1.AddResources(compiler); err != nil {
		return err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(deploymentSchemaJSON))
	if err != nil {
		return fmt.Errorf("解析集群部署 Schema: %w", err)
	}
	if err := compiler.AddResource(DeploymentSchemaURL, doc); err != nil {
		return fmt.Errorf("登记集群部署 Schema: %w", err)
	}
	return nil
}

func Parse(raw []byte) (Deployment, error) {
	sch, err := schema()
	if err != nil {
		return Deployment{}, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Deployment{}, fmt.Errorf("解析集群部署 JSON: %w", err)
	}
	if err := sch.Validate(instance); err != nil {
		return Deployment{}, fmt.Errorf("集群部署不符合 Schema: %w", err)
	}
	var deployment Deployment
	if err := json.Unmarshal(raw, &deployment); err != nil {
		return Deployment{}, fmt.Errorf("解析集群部署字段: %w", err)
	}
	deployment.Units, err = NormalizeServiceUnits(deployment.Units)
	if err != nil {
		return Deployment{}, err
	}
	profileIDs := map[string]struct{}{}
	for _, profile := range deployment.AppProfiles {
		if _, exists := profileIDs[profile.ID]; exists {
			return Deployment{}, fmt.Errorf("集群部署 App Profile id 重复: %q", profile.ID)
		}
		profileIDs[profile.ID] = struct{}{}
	}
	plugins := map[string]struct{}{}
	for _, unit := range deployment.Units {
		for _, plugin := range unit.Plugins {
			plugins[plugin.ID] = struct{}{}
			if _, ok := deployment.Resolution.PluginOrigins[plugin.ID]; !ok {
				return Deployment{}, fmt.Errorf("集群部署插件 %q 缺少 resolution.plugin_origins", plugin.ID)
			}
		}
	}
	for pluginID := range deployment.Resolution.PluginOrigins {
		if _, ok := plugins[pluginID]; !ok {
			return Deployment{}, fmt.Errorf("resolution.plugin_origins 包含未部署插件 %q", pluginID)
		}
	}
	return deployment, nil
}

// NormalizeServiceUnits 校验并规范化一组 ServiceUnit。组合输入与最终 Deployment
// 共用该函数，确保策略、插件重复和依赖 DAG 的语义只有一处定义。
func NormalizeServiceUnits(units []ServiceUnit) ([]ServiceUnit, error) {
	normalized := make([]ServiceUnit, len(units))
	copy(normalized, units)
	unitIDs := map[string]struct{}{}
	for i := range normalized {
		unit := &normalized[i]
		if _, exists := unitIDs[unit.ID]; exists {
			return nil, fmt.Errorf("集群部署 unit id 重复: %q", unit.ID)
		}
		unitIDs[unit.ID] = struct{}{}
		policy := servicemodel.Normalize(servicemodel.Policy{
			InstancePolicy: unit.InstancePolicy, StateModel: unit.StateModel,
			Visibility: unit.Visibility, Routing: unit.Routing, RoutingDomain: unit.RoutingDomain,
		})
		if err := servicemodel.Validate(policy); err != nil {
			return nil, fmt.Errorf("unit %q 运行策略无效: %w", unit.ID, err)
		}
		unit.InstancePolicy, unit.StateModel = policy.InstancePolicy, policy.StateModel
		unit.Visibility, unit.Routing = policy.Visibility, policy.Routing
		unit.RoutingDomain = policy.RoutingDomain
		if policy.InstancePolicy == servicemodel.PolicyLeader && unit.Replicas != 1 {
			return nil, fmt.Errorf("unit %q leader 当前只允许 replicas=1；高可用由节点失联后的重新调度提供", unit.ID)
		}
		if policy.InstancePolicy == servicemodel.PolicyPartitioned {
			if len(unit.PartitionKeys) == 0 {
				return nil, fmt.Errorf("unit %q partitioned 必须声明 partition_keys", unit.ID)
			}
			if unit.Replicas > len(unit.PartitionKeys) {
				return nil, fmt.Errorf("unit %q replicas 不能大于 partition_keys 数量", unit.ID)
			}
			seenPartitions := map[string]struct{}{}
			for _, key := range unit.PartitionKeys {
				if _, duplicate := seenPartitions[key]; duplicate {
					return nil, fmt.Errorf("unit %q partition_key 重复: %q", unit.ID, key)
				}
				seenPartitions[key] = struct{}{}
			}
		} else if len(unit.PartitionKeys) > 0 {
			return nil, fmt.Errorf("unit %q 只有 partitioned 策略可以声明 partition_keys", unit.ID)
		}
		if unit.Autoscaling != nil {
			if unit.Autoscaling.MinReplicas > unit.Autoscaling.MaxReplicas {
				return nil, fmt.Errorf("unit %q autoscaling min_replicas 不能大于 max_replicas", unit.ID)
			}
			if unit.Replicas < unit.Autoscaling.MinReplicas || unit.Replicas > unit.Autoscaling.MaxReplicas {
				return nil, fmt.Errorf("unit %q replicas 必须位于 autoscaling min/max 区间", unit.ID)
			}
		}
		pluginIDs := map[string]struct{}{}
		for j := range unit.Plugins {
			plugin := &unit.Plugins[j]
			if plugin.Channel == "" {
				plugin.Channel = "stable"
			}
			if _, exists := pluginIDs[plugin.ID]; exists {
				return nil, fmt.Errorf("unit %q 的插件 id 重复: %q", unit.ID, plugin.ID)
			}
			pluginIDs[plugin.ID] = struct{}{}
		}
	}
	graph := make(map[string][]string, len(normalized))
	for _, unit := range normalized {
		graph[unit.ID] = append([]string(nil), unit.DependsOn...)
	}
	if _, err := servicemodel.TopologicalOrder(graph); err != nil {
		return nil, fmt.Errorf("集群部署依赖图无效: %w", err)
	}
	return normalized, nil
}

func ParseFile(filename string) (Deployment, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return Deployment{}, fmt.Errorf("读取集群部署文件: %w", err)
	}
	return Parse(raw)
}

func (d Deployment) Digest() string {
	raw, _ := json.Marshal(d)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
