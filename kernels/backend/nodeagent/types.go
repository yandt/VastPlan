// Package nodeagent 把一份节点级 DesiredState 收敛为真实插件进程与实际态。
//
// 本包只依赖可替换的制品源、安装器、运行时和状态存储接口；本地文件和 NATS assignment
// 共享同一 reconcile 事务、回滚与故障恢复语义。
package nodeagent

import (
	"context"
	"encoding/json"
	"time"

	"cdsoft.com.cn/VastPlan/kernels/backend/pluginservice"
)

// ArtifactRepository 提供已经过索引绑定与 SHA-256 验证的制品字节。
type ArtifactRepository interface {
	Read(pluginservice.Ref) (pluginservice.Artifact, []byte, error)
}

// Installer 把不可变制品安装到本机内容寻址目录。
type Installer interface {
	Install(pluginservice.Artifact, []byte) (InstalledPlugin, error)
}

// GarbageCollector 是安装器的可选能力。只有一次 reconcile 完全收敛并持久化实际态后
// 才能清理未引用内容，失败候选和旧实例切换期间不得抢先删除。
type GarbageCollector interface {
	GarbageCollect(keepSHA256 []string) error
}

// Runtime 对一个 service unit 执行事务式替换。Apply 失败时旧实例必须继续运行。
type Runtime interface {
	Apply(context.Context, RuntimeUnit) error
	Stop(context.Context, string) error
	IsRunning(unitID, fingerprint string) bool
	Status(unitID string) (RuntimeStatus, bool)
	Events() <-chan RuntimeEvent
	UnitIDs() []string
	Close() error
}

// StateStore 持久化节点实际态；集群模式以本地文件为恢复真源并复制到 NATS。
type StateStore interface {
	Load() (ActualState, error)
	Save(ActualState) error
}

// InstalledPlugin 是经安装器校验后可交给 backend 宿主启动的插件。
type InstalledPlugin struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Channel   string `json:"channel"`
	SHA256    string `json:"sha256"`
	Root      string `json:"root"`
	EntryPath string `json:"entry_path"`
}

// RuntimeUnit 是运行时需要的完整、不可变组合。
type RuntimeUnit struct {
	ID          string
	Fingerprint string
	ServiceRole string
	Config      map[string]any
	Plugins     []InstalledPlugin
	RestartBase uint64
}

// RuntimeStatus 来自真实插件会话，不由持久化文件反推。
type RuntimeStatus struct {
	Healthy      bool
	PIDs         []int
	StartedAt    time.Time
	RestartCount uint64
}

// RuntimeEvent 通知 Agent 某 unit 的运行事实发生变化，使崩溃恢复无需等待配置轮询。
type RuntimeEvent struct {
	UnitID      string
	Fingerprint string
	Type        string
	Message     string
	OccurredAt  time.Time
}

// ActualState 是最近一次 reconcile 后持久化的节点视图。
type ActualState struct {
	Version          int                  `json:"version"`
	NodeID           string               `json:"node_id"`
	ObservedRevision uint64               `json:"observed_revision"`
	ObservedDigest   string               `json:"observed_digest"`
	AppliedRevision  uint64               `json:"applied_revision"`
	Units            map[string]UnitState `json:"units"`
	Errors           []OperationError     `json:"errors,omitempty"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

// UnitState 记录已成功切换的 service unit；失败升级不会覆盖此记录。
type UnitState struct {
	Fingerprint     string            `json:"fingerprint"`
	AppliedRevision uint64            `json:"applied_revision"`
	Status          string            `json:"status"`
	Plugins         []InstalledPlugin `json:"plugins"`
	PIDs            []int             `json:"pids,omitempty"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	RestartCount    uint64            `json:"restart_count"`
	LastError       string            `json:"last_error,omitempty"`
}

// OperationError 是可上报的阶段错误，Stage 用于区分 download/install/launch/stop。
type OperationError struct {
	UnitID  string `json:"unit_id"`
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

// Result 描述一次对账是否改变运行态以及是否完全收敛。
type Result struct {
	Changed   bool
	Converged bool
	State     ActualState
}

// RawConfig 深拷贝 JSON 配置，避免运行时持有期望态调用方仍可修改的 map。
func RawConfig(config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	raw, _ := json.Marshal(config)
	var clone map[string]any
	_ = json.Unmarshal(raw, &clone)
	return clone
}
