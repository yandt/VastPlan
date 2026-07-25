package nodeagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"github.com/nats-io/nats.go/jetstream"
)

type runningUnit struct {
	fingerprint      string
	host             *protocolbus.Host
	instances        []*protocolbus.PluginInstance
	registrations    []*addressing.Registration
	startedAt        time.Time
	restarts         uint64
	generation       uint64
	notified         bool
	leaderships      []*controlplane.Leadership
	plugins          []InstalledPlugin
	dependencyIssues []string
	spec             RuntimeUnit
}

// ProtocolRuntime 为每个 service unit 创建独立 backend 宿主。候选宿主先完成全部插件
// 握手和激活，再原子替换 map 中的当前实例，随后才关闭旧宿主。
type ProtocolRuntime struct {
	KernelVersion     string
	Logf              func(string, ...any)
	DependencyTimeout time.Duration
	Identity          string
	LeaderKV          jetstream.KeyValue

	mu              sync.RWMutex
	units           map[string]*runningUnit
	closed          bool
	events          chan RuntimeEvent
	nextID          uint64
	router          *addressing.Router
	Dependencies    kernelspi.Dependencies
	HostServices    map[string]protocolbus.HostService
	Drivers         *ExecutionDriverRegistry
	RuntimePools    *RuntimePoolManager
	ExecutionPolicy ExecutionPolicy
	HostingPolicy   RuntimeHostingPolicy
	ContextPolicy   ContextPolicy
	dynamicGoDriver PluginExecutionDriver
	PlacementPolicy PlacementPolicy
}

// AttachRouter 在首个 unit 启动前接入全局能力寻址。运行中切换 Router 会让已经发布的
// 租约和实际 handler 分离，因此明确拒绝这种隐式重配。
func (r *ProtocolRuntime) AttachRouter(router *addressing.Router) error {
	if router == nil {
		return errors.New("addressing router 不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("运行时已关闭")
	}
	if len(r.units) != 0 {
		return errors.New("已有 unit 运行时不能接入 addressing router")
	}
	r.router = router
	return nil
}

func NewProtocolRuntime(kernelVersion string, logf func(string, ...any)) *ProtocolRuntime {
	return &ProtocolRuntime{
		KernelVersion:     kernelVersion,
		Logf:              logf,
		DependencyTimeout: 5 * time.Second,
		Drivers:           DefaultExecutionDrivers(),
		RuntimePools:      NewRuntimePoolManager(logf),
		HostingPolicy:     RuntimeHostingPolicy{Default: RuntimeHostingShared},
		ContextPolicy:     DefaultContextPolicy(),
		units:             map[string]*runningUnit{},
		events:            make(chan RuntimeEvent, 64),
	}
}

func registerRuntimeHostServices(host *protocolbus.Host, services map[string]protocolbus.HostService) error {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "" || services[name] == nil {
			return errors.New("附加内核服务名称和实现不能为空")
		}
		if err := host.RegisterHostService(extpoint.KernelService, name, services[name]); err != nil {
			return err
		}
	}
	return nil
}

func newRuntimeInstanceID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "runtime-" + hex.EncodeToString(raw), nil
}

func cloneRuntimeUnit(unit RuntimeUnit) RuntimeUnit {
	unit.PartitionKeys = append([]string(nil), unit.PartitionKeys...)
	unit.EnvironmentAllowlists = cloneStringSlices(unit.EnvironmentAllowlists)
	unit.Plugins = append([]InstalledPlugin(nil), unit.Plugins...)
	for index := range unit.Plugins {
		unit.Plugins[index].Engines = cloneStringMap(unit.Plugins[index].Engines)
	}
	unit.Migrations = append([]StateMigrationPlan(nil), unit.Migrations...)
	unit.PartitionGenerations = cloneUint64Map(unit.PartitionGenerations)
	unit.PartitionFencingTokens = cloneStringMap(unit.PartitionFencingTokens)
	unit.Config = RawConfig(unit.Config)
	return unit
}

func cloneUint64Map(input map[string]uint64) map[string]uint64 {
	if input == nil {
		return nil
	}
	out := make(map[string]uint64, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneStringSlices(input map[string][]string) map[string][]string {
	if input == nil {
		return nil
	}
	out := make(map[string][]string, len(input))
	for key, values := range input {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (r *ProtocolRuntime) Stop(ctx context.Context, unitID string) error {
	r.mu.Lock()
	unit, ok := r.units[unitID]
	if ok {
		delete(r.units, unitID)
	}
	r.mu.Unlock()
	if ok {
		closeRegistrations(ctx, unit.registrations)
		if err := unit.host.Drain(ctx); err != nil && r.Logf != nil {
			r.Logf("unit %s drain 未完整完成，将强制回收: %v", unitID, err)
		}
		unit.host.Stop()
		for _, leadership := range unit.leaderships {
			if err := leadership.Close(ctx); err != nil && r.Logf != nil {
				r.Logf("unit %s 释放 leader lease 失败: %v", unitID, err)
			}
		}
	}
	return nil
}

func (r *ProtocolRuntime) IsRunning(unitID, fingerprint string) bool {
	status, ok := r.Status(unitID)
	if !ok || !status.Healthy {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	unit, ok := r.units[unitID]
	return ok && unit.fingerprint == fingerprint
}

func (r *ProtocolRuntime) Status(unitID string) (RuntimeStatus, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	unit, ok := r.units[unitID]
	if !ok {
		return RuntimeStatus{}, false
	}
	status := RuntimeStatus{
		Healthy:          len(unit.instances) > 0,
		Readiness:        "ready",
		DependencyIssues: append([]string(nil), unit.dependencyIssues...),
		StartedAt:        unit.startedAt,
		RestartCount:     unit.restarts,
	}
	seenPIDs := map[int]struct{}{}
	for _, instance := range unit.instances {
		if instance.PID > 0 {
			if _, duplicate := seenPIDs[instance.PID]; !duplicate {
				seenPIDs[instance.PID] = struct{}{}
				status.PIDs = append(status.PIDs, instance.PID)
			}
		}
		if !instance.Alive() {
			status.Healthy = false
		}
	}
	sort.Ints(status.PIDs)
	if len(status.DependencyIssues) > 0 {
		status.Readiness = "degraded"
	}
	return status, true
}

func (r *ProtocolRuntime) Events() <-chan RuntimeEvent { return r.events }

func (r *ProtocolRuntime) UnitIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.units))
	for id := range r.units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Host 暴露只读宿主句柄，供内核服务层和端到端测试调用当前 unit 的贡献。
func (r *ProtocolRuntime) Host(unitID string) (*protocolbus.Host, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	unit, ok := r.units[unitID]
	if !ok {
		return nil, false
	}
	return unit.host, true
}

func (r *ProtocolRuntime) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	units := r.units
	r.units = map[string]*runningUnit{}
	r.mu.Unlock()
	for _, unit := range units {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		closeRegistrations(ctx, unit.registrations)
		_ = unit.host.Drain(ctx)
		cancel()
		unit.host.Stop()
		for _, leadership := range unit.leaderships {
			_ = leadership.Close(context.Background())
		}
	}
	if r.RuntimePools != nil {
		return r.RuntimePools.Close()
	}
	return nil
}
