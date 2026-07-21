// Package nodeagent 把一份节点级 DesiredState 收敛为真实插件实例与实际态。
//
// 本包只依赖可替换的制品源、安装器、运行时和状态存储接口；本地文件和 NATS assignment
// 共享同一 reconcile 事务、回滚与故障恢复语义。
package nodeagent

import (
	"context"
	"encoding/json"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

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

// StateStore 持久化节点实际态；集群模式以本地文件为恢复真源并复制到 NATS。
type StateStore interface {
	Load() (ActualState, error)
	Save(ActualState) error
}

// InstalledPlugin 是经安装器校验后可交给 backend 宿主启动的插件。
type InstalledPlugin struct {
	ID            string                    `json:"id"`
	Publisher     string                    `json:"publisher"`
	Version       string                    `json:"version"`
	Engines       map[string]string         `json:"engines,omitempty"`
	Channel       string                    `json:"channel"`
	SHA256        string                    `json:"sha256"`
	Root          string                    `json:"root"`
	EntryPath     string                    `json:"entry_path"`
	DynamicGoPath string                    `json:"dynamic_go_path,omitempty"`
	Execution     pluginv1.BackendExecution `json:"execution"`
	State         *PluginStateContract      `json:"state,omitempty"`
	Contract      PluginRuntimeContract     `json:"contract"`
}

// PluginRuntimeContract 是安装时从已验签清单冻结的运行授权，宿主不再相信进程自报。
type PluginRuntimeContract struct {
	Contributions  []pluginv1.RuntimeContribution `json:"contributions"`
	Requires       []pluginv1.RuntimeRequirement  `json:"requires,omitempty"`
	KernelServices []string                       `json:"kernel_services,omitempty"`
	ContextAccess  pluginv1.ContextAccess         `json:"context_access,omitempty"`
}

// PluginStateIdentity 只标识插件私有状态格式，不暴露其存储结构。
type PluginStateIdentity struct {
	Format        string `json:"format"`
	FormatVersion int32  `json:"format_version"`
}

func pluginStateIdentity(identity pluginv1.StateIdentity) PluginStateIdentity {
	return PluginStateIdentity{Format: identity.Format, FormatVersion: identity.FormatVersion}
}

func (i PluginStateIdentity) contractIdentity() pluginv1.StateIdentity {
	return pluginv1.StateIdentity{Format: i.Format, FormatVersion: i.FormatVersion}
}

// PluginStateContract 随已安装制品持久化，使下一次升级无需重新信任外部清单。
type PluginStateContract struct {
	PluginStateIdentity
	MigrationProtocol string                `json:"migration_protocol,omitempty"`
	MigrationFrom     []PluginStateIdentity `json:"migration_from,omitempty"`
}

// RuntimeUnit 是运行时需要的完整、不可变组合。
type RuntimeUnit struct {
	ID                     string
	Fingerprint            string
	ServiceRole            string
	LogicalService         string
	InstancePolicy         string
	StateModel             string
	Visibility             string
	Routing                string
	RoutingDomain          string
	Generation             uint64
	FencingToken           string
	PartitionKeys          []string
	PartitionGenerations   map[string]uint64
	PartitionFencingTokens map[string]string
	EnvironmentAllowlists  map[string][]string
	Config                 map[string]any
	Plugins                []InstalledPlugin
	Migrations             []StateMigrationPlan
	RestartBase            uint64
}

// StateMigrationPlan 由 Reconciler 从旧/新签名清单计算，Runtime 只负责按协议执行。
// TransactionID 对同一次逻辑升级稳定，插件可据此实现幂等 prepare/commit/rollback。
type StateMigrationPlan struct {
	PluginID      string
	TransactionID string
	From          PluginStateIdentity
	To            PluginStateIdentity
}

// RuntimeStatus 来自真实插件会话，不由持久化文件反推。
type RuntimeStatus struct {
	Healthy          bool
	Readiness        string
	DependencyIssues []string
	PIDs             []int
	StartedAt        time.Time
	RestartCount     uint64
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
	Fingerprint      string            `json:"fingerprint"`
	AppliedRevision  uint64            `json:"applied_revision"`
	Phase            UnitPhase         `json:"phase"`
	PhaseChangedAt   time.Time         `json:"phase_changed_at"`
	Plugins          []InstalledPlugin `json:"plugins"`
	PIDs             []int             `json:"pids,omitempty"`
	StartedAt        *time.Time        `json:"started_at,omitempty"`
	RestartCount     uint64            `json:"restart_count"`
	LastError        string            `json:"last_error,omitempty"`
	Readiness        string            `json:"readiness,omitempty"`
	DependencyIssues []string          `json:"dependency_issues,omitempty"`
	Candidate        *CandidateState   `json:"candidate,omitempty"`
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
