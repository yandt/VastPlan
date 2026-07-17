// Package registry 实现扩展点注册表（Extension Point Registry）。
//
// 扩展点 = 一个具名 Registry + 一份贡献契约（系统架构 §1.5）。
// 插件经协议总线注册贡献，宿主在调用时查表分发。
// 支持运行时注册/解绑以支撑热装（ADR-0003）。
package registry

import (
	"fmt"
	"sort"
	"sync"
)

// Dispatch 分发语义（插件契约与协议 第四章 §4.2）。
type Dispatch string

const (
	DispatchSingle Dispatch = "single" // 单一提供者：一个 id 由唯一贡献提供
	DispatchSelect Dispatch = "select" // 择一：按 priority 匹配，首个决定性结果即止
	DispatchFanout Dispatch = "fanout" // 扇出：所有匹配贡献都收到
	DispatchMount  Dispatch = "mount"  // 登记挂载：注册即呈现，非请求响应式
)

// ExtensionPoint 扩展点定义（内核开放的"可被插件填充的位置"）。
type ExtensionPoint struct {
	Name     string // 如 tool.package（= CallTarget.extension_point）
	Dispatch Dispatch
}

// Contribution 一条已注册的贡献。
type Contribution struct {
	ExtensionPoint string
	ID             string // 稳定逻辑名（= CallTarget.capability），四处同名
	PluginID       string // 提供它的插件（用于崩溃时批量摘除）
	Priority       int
	Descriptor     []byte // 该扩展点要求的贡献契约（JSON）
}

// Registry 登记扩展点与其上贡献，提供注册/解绑/查询。
type Registry struct {
	mu     sync.RWMutex
	points map[string]ExtensionPoint
	// extensionPoint -> id -> contribution
	contribs map[string]map[string]Contribution
}

func New() *Registry {
	return &Registry{
		points:   make(map[string]ExtensionPoint),
		contribs: make(map[string]map[string]Contribution),
	}
}

// DefinePoint 内核声明一个扩展点。
func (r *Registry) DefinePoint(p ExtensionPoint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.points[p.Name] = p
	if _, ok := r.contribs[p.Name]; !ok {
		r.contribs[p.Name] = make(map[string]Contribution)
	}
}

// Points 返回已定义的扩展点（按名排序）。
func (r *Registry) Points() []ExtensionPoint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ExtensionPoint, 0, len(r.points))
	for _, p := range r.points {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Register 注册一条贡献。未定义的扩展点或 single 语义下的重复 id 会被拒绝（fail-closed）。
func (r *Registry) Register(c Contribution) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.points[c.ExtensionPoint]
	if !ok {
		return fmt.Errorf("未定义的扩展点 %q", c.ExtensionPoint)
	}
	if c.ID == "" {
		return fmt.Errorf("贡献 id 不能为空")
	}
	if exist, dup := r.contribs[c.ExtensionPoint][c.ID]; dup {
		if p.Dispatch == DispatchSingle || exist.PluginID != c.PluginID {
			return fmt.Errorf("扩展点 %q 的能力 id %q 已由插件 %q 占用",
				c.ExtensionPoint, c.ID, exist.PluginID)
		}
	}
	r.contribs[c.ExtensionPoint][c.ID] = c
	return nil
}

// Unregister 精确摘除一条属于指定插件的贡献。pluginID 是所有权条件，防止一个
// 插件通过动态协议卸载另一个插件的能力。不存在或所有权不匹配时返回 false。
func (r *Registry) Unregister(extensionPoint, id, pluginID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.contribs[extensionPoint]
	if !ok {
		return false
	}
	contribution, ok := m[id]
	if !ok || contribution.PluginID != pluginID {
		return false
	}
	delete(m, id)
	return true
}

// Lookup 按 (扩展点, 能力 id) 查一条贡献——single 语义的解析路径。
func (r *Registry) Lookup(extensionPoint, id string) (Contribution, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.contribs[extensionPoint]
	if !ok {
		return Contribution{}, false
	}
	c, ok := m[id]
	return c, ok
}

// List 列出某扩展点上的全部贡献，按 priority 降序——fanout/select 的分发路径。
func (r *Registry) List(extensionPoint string) []Contribution {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Contribution, 0, len(r.contribs[extensionPoint]))
	for _, c := range r.contribs[extensionPoint] {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// UnregisterPlugin 摘除某插件的全部贡献——插件崩溃/停用时调用（ADR-0004 故障隔离）。
// 返回被摘除的贡献数。
func (r *Registry) UnregisterPlugin(pluginID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for ep, m := range r.contribs {
		for id, c := range m {
			if c.PluginID == pluginID {
				delete(r.contribs[ep], id)
				n++
			}
		}
	}
	return n
}
