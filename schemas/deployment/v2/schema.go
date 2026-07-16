// Package deploymentv2 定义控制面多节点 service 部署规格。
//
// v2 是全局调度输入；调度器把它展开成每节点一份 deployment/v1 执行快照，Node Agent
// 因而不承担集群仲裁，也不会因为多个节点各自计算而重复启动同一副本。
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
	Version  int                   `json:"version"`
	Revision uint64                `json:"revision"`
	Metadata deploymentv1.Metadata `json:"metadata"`
	Units    []ServiceUnit         `json:"units"`
}

type ServiceUnit struct {
	ID          string                   `json:"id"`
	Kind        string                   `json:"kind"`
	Plugins     []deploymentv1.PluginRef `json:"plugins"`
	Config      map[string]any           `json:"config,omitempty"`
	Enabled     bool                     `json:"enabled"`
	ServiceRole string                   `json:"service_role"`
	Replicas    int                      `json:"replicas"`
	Autoscaling *Autoscaling             `json:"autoscaling,omitempty"`
	Resources   ResourceRequirements     `json:"resources,omitempty"`
	Placement   Placement                `json:"placement,omitempty"`
}

// ResourceList 使用规范化整数，避免不同组件对 "500m"、"2Gi" 等文本单位产生歧义。
type ResourceList struct {
	CPUMillis   int64 `json:"cpu_millis,omitempty"`
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
	GPU         int64 `json:"gpu,omitempty"`
}

type ResourceRequirements struct {
	Requests ResourceList `json:"requests,omitempty"`
}

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
		if err := commonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(deploymentSchemaJSON))
		if err != nil {
			compileErr = fmt.Errorf("解析集群部署 Schema: %w", err)
			return
		}
		if err := compiler.AddResource(DeploymentSchemaURL, doc); err != nil {
			compileErr = fmt.Errorf("登记集群部署 Schema: %w", err)
			return
		}
		deploymentSchema, compileErr = compiler.Compile(DeploymentSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译集群部署 Schema: %w", compileErr)
		}
	})
	return deploymentSchema, compileErr
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
	unitIDs := map[string]struct{}{}
	for i := range deployment.Units {
		unit := &deployment.Units[i]
		if _, exists := unitIDs[unit.ID]; exists {
			return Deployment{}, fmt.Errorf("集群部署 unit id 重复: %q", unit.ID)
		}
		unitIDs[unit.ID] = struct{}{}
		if unit.Autoscaling != nil {
			if unit.Autoscaling.MinReplicas > unit.Autoscaling.MaxReplicas {
				return Deployment{}, fmt.Errorf("unit %q autoscaling min_replicas 不能大于 max_replicas", unit.ID)
			}
			if unit.Replicas < unit.Autoscaling.MinReplicas || unit.Replicas > unit.Autoscaling.MaxReplicas {
				return Deployment{}, fmt.Errorf("unit %q replicas 必须位于 autoscaling min/max 区间", unit.ID)
			}
		}
		pluginIDs := map[string]struct{}{}
		for j := range unit.Plugins {
			plugin := &unit.Plugins[j]
			if plugin.Channel == "" {
				plugin.Channel = "stable"
			}
			if _, exists := pluginIDs[plugin.ID]; exists {
				return Deployment{}, fmt.Errorf("unit %q 的插件 id 重复: %q", unit.ID, plugin.ID)
			}
			pluginIDs[plugin.ID] = struct{}{}
		}
	}
	return deployment, nil
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
