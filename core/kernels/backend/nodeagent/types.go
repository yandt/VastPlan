// Package nodeagent 把一份节点级 DesiredState 收敛为真实插件实例与实际态。
//
// 本包只依赖可替换的制品源、安装器、运行时和状态存储接口；本地文件和 NATS assignment
// 共享同一 reconcile 事务、回滚与故障恢复语义。
package nodeagent

import (
	"context"
	"errors"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent/model"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

var ErrActivationDeferred = errors.New("runtime activation deferred")

// Installer 把不可变制品安装到本机内容寻址目录。
type Installer interface {
	Install(VerifiedArtifact) (InstalledPlugin, error)
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

// ActivationGate is selected once by the composition root. It can delay a
// prepared unit without teaching the Reconciler about a concrete bootstrap or
// storage implementation.
type ActivationGate interface {
	Allow(context.Context, RuntimeUnit) error
}

// StateStore 持久化节点实际态；集群模式以本地文件为恢复真源并复制到 NATS。
type StateStore interface {
	Load() (ActualState, error)
	Save(ActualState) error
}

type InstalledPlugin = model.InstalledPlugin
type PluginRuntimeContract = model.PluginRuntimeContract
type PluginStateIdentity = model.PluginStateIdentity
type PluginStateContract = model.PluginStateContract
type RuntimeUnit = model.RuntimeUnit
type ReplacementCandidate = model.ReplacementCandidate
type ReplacementReadinessBarrier = model.ReplacementReadinessBarrier
type StateMigrationPlan = model.StateMigrationPlan
type StateMigrationError = model.StateMigrationError
type RuntimeStatus = model.RuntimeStatus
type RuntimeEvent = model.RuntimeEvent

func pluginStateIdentity(identity pluginv1.StateIdentity) PluginStateIdentity {
	return model.PluginStateIdentityFromContract(identity)
}

// ActualState 是最近一次 reconcile 后持久化的节点视图。
type ActualState struct {
	Version                  int                  `json:"version"`
	NodeID                   string               `json:"node_id"`
	ObservedRevision         uint64               `json:"observed_revision"`
	ObservedDigest           string               `json:"observed_digest"`
	AppliedRevision          uint64               `json:"applied_revision"`
	ReferenceTenant          string               `json:"reference_tenant,omitempty"`
	ReferenceOwnerID         string               `json:"reference_owner_id,omitempty"`
	ReferenceGeneration      uint64               `json:"reference_generation,omitempty"`
	ReferenceDesiredRevision uint64               `json:"reference_desired_revision,omitempty"`
	ReferencePending         bool                 `json:"reference_pending,omitempty"`
	ReferencePublishedAt     time.Time            `json:"reference_published_at,omitempty"`
	BootstrapGeneration      uint64               `json:"bootstrap_generation,omitempty"`
	BootstrapPublishedAt     time.Time            `json:"bootstrap_published_at,omitempty"`
	Units                    map[string]UnitState `json:"units"`
	Errors                   []OperationError     `json:"errors,omitempty"`
	UpdatedAt                time.Time            `json:"updated_at"`
}

// UnitState 同时记录当前稳定实例和可选的升级候选。候选失败不会覆盖当前实例，
// 控制面因此能区分“当前实例失效”和“新版本尝试失败”两种完全不同的事实。
type UnitState struct {
	Fingerprint         string              `json:"fingerprint"`
	AppliedRevision     uint64              `json:"applied_revision"`
	Phase               UnitPhase           `json:"phase"`
	PhaseChangedAt      time.Time           `json:"phase_changed_at"`
	Plugins             []InstalledPlugin   `json:"plugins"`
	PIDs                []int               `json:"pids,omitempty"`
	StartedAt           *time.Time          `json:"started_at,omitempty"`
	RestartCount        uint64              `json:"restart_count"`
	LastError           string              `json:"last_error,omitempty"`
	Readiness           string              `json:"readiness,omitempty"`
	DependencyIssues    []string            `json:"dependency_issues,omitempty"`
	KernelServiceGrants map[string][]string `json:"kernel_service_grants,omitempty"`
	Candidate           *CandidateState     `json:"candidate,omitempty"`
}

// CandidateState 描述尚未替换当前实例的候选组合。Plugins 只有在制品全部安装并
// 校验后才出现；PhaseFailed 会保留失败原因，供控制面诊断和下一轮对账覆盖。
type CandidateState struct {
	Fingerprint    string            `json:"fingerprint"`
	Phase          UnitPhase         `json:"phase"`
	PhaseChangedAt time.Time         `json:"phase_changed_at"`
	Plugins        []InstalledPlugin `json:"plugins,omitempty"`
	LastError      string            `json:"last_error,omitempty"`
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

// ArtifactReferencePublisher writes one complete, sealed Assignment snapshot
// to the managed repository. Implementations must preserve the authenticated
// Node Agent system identity across the cluster hop.
type ArtifactReferencePublisher interface {
	Publish(context.Context, string, pluginv1.ArtifactReferenceSnapshot) error
}

// BootstrapUpgradeCoordinator is a trusted-host capability. It mirrors only
// verified critical candidates into offline Seed during prepare, then advances
// LKG after runtime health and Assignment reference publication succeed.
type BootstrapUpgradeCoordinator interface {
	Begin([]bootstrapinventory.Item) (bootstrapinventory.Inventory, error)
	Prepare(context.Context, []VerifiedArtifact) (bootstrapinventory.Inventory, error)
	Commit(context.Context) (bootstrapinventory.Inventory, error)
}

// RawConfig 深拷贝 JSON 配置，避免运行时持有期望态调用方仍可修改的 map。
func RawConfig(config map[string]any) map[string]any {
	return model.RawConfig(config)
}

func cloneStringSlices(input map[string][]string) map[string][]string {
	return model.CloneStringSlices(input)
}

func cloneStringMap(input map[string]string) map[string]string {
	return model.CloneStringMap(input)
}
