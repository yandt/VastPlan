// Package extpoint 定义各扩展点的 descriptor 契约与判定语义。
//
// 宿主按它**分发**（该把调用发给谁、按什么顺序、如何合成结论），
// 插件按它**声明**（我订阅什么、我的适用范围是什么）——两边共用同一份定义，
// 避免各写一份而漂移。规格见 docs/dev/architecture/插件契约与协议.md 第四章。
package extpoint

import (
	"encoding/json"
	"path"
)

// 扩展点名（= CallTarget.extension_point，四处同名之一）。
const (
	ToolPackage       = "tool.package"
	PermissionChecker = "permission.checker"
	EventSink         = "event.sink"
	Hook              = "hook"
	KernelService     = "kernel.service"
)

// ── permission.checker（select 语义）─────────────────────

// Decision 权限判定结论（§4.3）。
type Decision string

const (
	// DecisionAllow 放行，且到此定论——后续校验器不再被问。
	DecisionAllow Decision = "allow"
	// DecisionDeny 拒绝，且到此定论。
	DecisionDeny Decision = "deny"
	// DecisionAbstain 弃权：本校验器不表态，交给下一个（优先级更低者）。
	DecisionAbstain Decision = "abstain"
)

// CheckerDescriptor permission.checker 的贡献契约。
type CheckerDescriptor struct {
	Title string `json:"title,omitempty"`
	// Applies 适用范围预筛（可选）：不匹配则宿主直接跳过，不必往返一次调用。
	// 留空表示"任何调用都问我"，由校验器自行 abstain。
	Applies *Applies `json:"applies,omitempty"`
}

// Applies 按三元组 (caller, scene, target) 预筛，值为 glob（如 `agent.*`）。
type Applies struct {
	Caller string `json:"caller,omitempty"` // 匹配 CallerKind，如 CALLER_KIND_AGENT
	Scene  string `json:"scene,omitempty"`
	Target string `json:"target,omitempty"` // 匹配被检查调用的 capability
}

// PermissionRequest 宿主发给校验器的 payload：被检查调用的目标。
// 三元组的另两元（caller、scene）在 CallContext 里——宿主原样透传被检查调用的上下文，
// 故校验器看到的就是发起方的真实身份与场景。
type PermissionRequest struct {
	ExtensionPoint string `json:"extensionPoint"`
	Capability     string `json:"capability"`
	Operation      string `json:"operation,omitempty"`
}

// PermissionResponse 校验器的回答。
type PermissionResponse struct {
	Decision Decision `json:"decision"`
	Reason   string   `json:"reason,omitempty"`
}

// Matches 判断某次调用是否落在本校验器的适用范围内。
func (d *CheckerDescriptor) Matches(callerKind, scene, capability string) bool {
	if d == nil || d.Applies == nil {
		return true // 未声明范围 = 任何调用都问它
	}
	a := d.Applies
	return globMatch(a.Caller, callerKind) && globMatch(a.Scene, scene) && globMatch(a.Target, capability)
}

// ── event.sink（fanout 语义）────────────────────────────

// SinkDescriptor event.sink 的贡献契约。
type SinkDescriptor struct {
	Title string `json:"title,omitempty"`
	// Subscribe 订阅的事件类型 glob 列表，如 ["task.*", "plugin.activated"]。
	// 留空表示不订阅任何事件（而非订阅全部）——fail-closed：没声明就别收。
	Subscribe []string `json:"subscribe,omitempty"`
}

// Subscribes 判断该 sink 是否订阅了此事件类型。
func (d *SinkDescriptor) Subscribes(eventType string) bool {
	if d == nil {
		return false
	}
	for _, pat := range d.Subscribe {
		if globMatch(pat, eventType) {
			return true
		}
	}
	return false
}

// ── 解析 ────────────────────────────────────────────────

// ParseChecker 解析 permission.checker 的 descriptor；解析失败返回零值（=范围不限）。
func ParseChecker(raw []byte) (*CheckerDescriptor, error) {
	d := &CheckerDescriptor{}
	if len(raw) == 0 {
		return d, nil
	}
	if err := json.Unmarshal(raw, d); err != nil {
		return nil, err
	}
	return d, nil
}

// ParseSink 解析 event.sink 的 descriptor。
func ParseSink(raw []byte) (*SinkDescriptor, error) {
	d := &SinkDescriptor{}
	if len(raw) == 0 {
		return d, nil
	}
	if err := json.Unmarshal(raw, d); err != nil {
		return nil, err
	}
	return d, nil
}

// globMatch 空模式视为"不限"；否则按 glob 匹配（`*` 匹配任意片段）。
func globMatch(pattern, s string) bool {
	if pattern == "" {
		return true
	}
	ok, err := path.Match(pattern, s)
	if err != nil {
		return false // 非法模式 → 不匹配（fail-closed）
	}
	return ok
}
