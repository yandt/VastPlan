package nodeagent

import (
	"context"
	"fmt"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

func (r *ProtocolRuntime) monitor(unitID string, generation uint64, instance *protocolbus.PluginInstance) {
	<-instance.Done()
	r.mu.Lock()
	unit, ok := r.units[unitID]
	if !ok || unit.generation != generation || unit.notified {
		r.mu.Unlock()
		return
	}
	unit.notified = true
	event := RuntimeEvent{
		UnitID:      unitID,
		Fingerprint: unit.fingerprint,
		Type:        "instance_exited",
		Message:     fmt.Sprint(instance.Err()),
		OccurredAt:  time.Now().UTC(),
	}
	r.mu.Unlock()
	select {
	case r.events <- event:
	default:
		if r.Logf != nil {
			r.Logf("运行时事件队列已满，丢弃 unit=%s type=%s", event.UnitID, event.Type)
		}
	}
}

func (r *ProtocolRuntime) monitorDependencies(unitID string, generation uint64) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		r.mu.RLock()
		unit, ok := r.units[unitID]
		if !ok || unit.generation != generation {
			r.mu.RUnlock()
			return
		}
		plugins := append([]InstalledPlugin(nil), unit.plugins...)
		r.mu.RUnlock()
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		degraded, err := validateRuntimeRequirements(ctx, plugins, r.router, 150*time.Millisecond)
		cancel()
		if err != nil {
			if r.Logf != nil {
				r.Logf("unit %s 依赖丢失，停止数据面: %v", unitID, err)
			}
			_ = r.Stop(context.Background(), unitID)
			select {
			case r.events <- RuntimeEvent{UnitID: unitID, Fingerprint: unit.fingerprint, Type: "dependency_lost", Message: err.Error(), OccurredAt: time.Now().UTC()}:
			default:
			}
			return
		}
		r.mu.Lock()
		if current, exists := r.units[unitID]; exists && current.generation == generation {
			current.dependencyIssues = degraded
		}
		r.mu.Unlock()
	}
}

func (r *ProtocolRuntime) monitorLeadership(unitID string, generation uint64, leadership *controlplane.Leadership) {
	var err error
	select {
	case err = <-leadership.Lost():
		if err == nil {
			return
		}
	case <-leadership.Done():
		return
	}
	r.mu.RLock()
	unit, current := r.units[unitID]
	valid := current && unit.generation == generation && containsLeadership(unit.leaderships, leadership)
	r.mu.RUnlock()
	if !valid {
		return
	}
	if r.Logf != nil {
		r.Logf("unit %s 失去 leader fencing，立即停止数据面: %v", unitID, err)
	}
	select {
	case r.events <- RuntimeEvent{UnitID: unitID, Fingerprint: unit.fingerprint, Type: "leadership_lost", Message: err.Error(), OccurredAt: time.Now().UTC()}:
	default:
	}
	_ = r.Stop(context.Background(), unitID)
}

func containsLeadership(all []*controlplane.Leadership, target *controlplane.Leadership) bool {
	for _, leadership := range all {
		if leadership == target {
			return true
		}
	}
	return false
}
