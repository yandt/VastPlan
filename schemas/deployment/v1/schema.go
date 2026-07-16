// Package deploymentv1 定义单节点自动装配使用的 DesiredState v1 契约。
//
// v1 只接受 service 单元和固定 replicas=1；资源请求由 v2 控制器复制进来用于全局
// 占用核算，Node Agent 不在本地重新调度。portal/app、多副本与亲和规则只属于 v2。
package deploymentv1

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	commonv1 "cdsoft.com.cn/VastPlan/schemas/common/v1"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// DesiredStateSchemaURL 是期望态 v1 Schema 的稳定标识。
const DesiredStateSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/deployment/v1/vastplan.desired-state.schema.json"

//go:embed vastplan.desired-state.schema.json
var desiredStateSchemaJSON []byte

var (
	compileOnce sync.Once
	desiredSch  *jsonschema.Schema
	compileErr  error
)

// DesiredState 是节点代理接收的一份完整期望态快照。
type DesiredState struct {
	Version  int      `json:"version"`
	Revision uint64   `json:"revision"`
	Metadata Metadata `json:"metadata"`
	Units    []Unit   `json:"units"`
}

// Metadata 标识配置集及其可选租户边界。
type Metadata struct {
	Name   string `json:"name"`
	Tenant string `json:"tenant,omitempty"`
}

// Unit 是 v1 唯一支持的 service 组合单元。
type Unit struct {
	ID          string               `json:"id"`
	Kind        string               `json:"kind"`
	Plugins     []PluginRef          `json:"plugins"`
	Config      map[string]any       `json:"config,omitempty"`
	Enabled     bool                 `json:"enabled"`
	ServiceRole string               `json:"service_role"`
	Replicas    int                  `json:"replicas"`
	Placement   Placement            `json:"placement,omitempty"`
	Resources   ResourceRequirements `json:"resources,omitempty"`
}

// PluginRef 通过不可变制品三元组引用一个插件；channel 留空时规范化为 stable。
type PluginRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Channel string `json:"channel,omitempty"`
}

// Placement 在本地版本只实现标签全匹配的 nodeSelector。
type Placement struct {
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

type ResourceList struct {
	CPUMillis   int64 `json:"cpu_millis,omitempty"`
	MemoryBytes int64 `json:"memory_bytes,omitempty"`
	GPU         int64 `json:"gpu,omitempty"`
}

// ResourceRequirements 是控制器已决策后的资源占用凭据，不授权 Node Agent 改变落点。
type ResourceRequirements struct {
	Requests ResourceList `json:"requests,omitempty"`
}

func schema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := commonv1.AddResources(compiler); err != nil {
			compileErr = err
			return
		}
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(desiredStateSchemaJSON))
		if err != nil {
			compileErr = fmt.Errorf("解析期望态 Schema: %w", err)
			return
		}
		if err := compiler.AddResource(DesiredStateSchemaURL, doc); err != nil {
			compileErr = fmt.Errorf("登记期望态 Schema: %w", err)
			return
		}
		desiredSch, compileErr = compiler.Compile(DesiredStateSchemaURL)
		if compileErr != nil {
			compileErr = fmt.Errorf("编译期望态 Schema: %w", compileErr)
		}
	})
	return desiredSch, compileErr
}

// Parse 校验、解析并规范化一份期望态。JSON Schema 负责结构，Go 语义校验负责数组内 ID 唯一性。
func Parse(raw []byte) (DesiredState, error) {
	sch, err := schema()
	if err != nil {
		return DesiredState{}, err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return DesiredState{}, fmt.Errorf("解析期望态 JSON: %w", err)
	}
	if err := sch.Validate(instance); err != nil {
		return DesiredState{}, fmt.Errorf("期望态不符合 Schema: %w", err)
	}
	var state DesiredState
	if err := json.Unmarshal(raw, &state); err != nil {
		return DesiredState{}, fmt.Errorf("解析期望态字段: %w", err)
	}
	unitIDs := map[string]struct{}{}
	for i := range state.Units {
		unit := &state.Units[i]
		if _, exists := unitIDs[unit.ID]; exists {
			return DesiredState{}, fmt.Errorf("期望态 unit id 重复: %q", unit.ID)
		}
		unitIDs[unit.ID] = struct{}{}
		pluginIDs := map[string]struct{}{}
		for j := range unit.Plugins {
			plugin := &unit.Plugins[j]
			if plugin.Channel == "" {
				plugin.Channel = "stable"
			}
			if _, exists := pluginIDs[plugin.ID]; exists {
				return DesiredState{}, fmt.Errorf("unit %q 的插件 id 重复: %q", unit.ID, plugin.ID)
			}
			pluginIDs[plugin.ID] = struct{}{}
		}
	}
	return state, nil
}

// ParseFile 从本地 config-as-code 文件读取当前快照。
func ParseFile(filename string) (DesiredState, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return DesiredState{}, fmt.Errorf("读取期望态文件: %w", err)
	}
	return Parse(raw)
}

// MatchesNode 判断 service unit 是否应落到当前节点。选择器为空表示本地单节点可运行。
func (u Unit) MatchesNode(labels map[string]string) bool {
	for key, want := range u.Placement.NodeSelector {
		if labels[key] != want {
			return false
		}
	}
	return true
}

// Fingerprint 是运行时组合的内容指纹，不包含 revision。回滚到相同组合时可识别已在运行的实例，
// revision 只负责记录配置历史，不应导致无意义重启。
func (u Unit) Fingerprint() string {
	plugins := append([]PluginRef(nil), u.Plugins...)
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].ID < plugins[j].ID })
	copy := u
	copy.Plugins = plugins
	raw, _ := json.Marshal(copy)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// Digest 标识整份期望态的规范化内容。Node Agent 用它拒绝“同 revision、不同内容”
// 的配置覆盖；显式回滚到较小 revision 则仍被允许。
func (s DesiredState) Digest() string {
	raw, _ := json.Marshal(s)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
