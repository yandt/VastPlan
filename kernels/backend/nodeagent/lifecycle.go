package nodeagent

import "fmt"

// UnitPhase 是 Backend 1.0 对外报告的插件单元生命周期状态。不存在于 Units map
// 表示尚未安装；removed 会在删除记录前形成一个可观察检查点。
type UnitPhase string

const (
	PhaseUninstalled       UnitPhase = "uninstalled"
	PhaseInstalledInactive UnitPhase = "installed_inactive"
	PhaseActivating        UnitPhase = "activating"
	PhaseActive            UnitPhase = "active"
	PhaseDraining          UnitPhase = "draining"
	PhaseDeactivating      UnitPhase = "deactivating"
	PhaseFailed            UnitPhase = "failed"
	PhaseRemoved           UnitPhase = "removed"
)

var lifecycleTransitions = map[UnitPhase]map[UnitPhase]struct{}{
	"": {
		PhaseUninstalled: {}, PhaseActive: {}, PhaseFailed: {},
	},
	PhaseUninstalled: {
		PhaseInstalledInactive: {}, PhaseFailed: {}, PhaseRemoved: {},
	},
	PhaseInstalledInactive: {
		PhaseActivating: {}, PhaseDeactivating: {}, PhaseRemoved: {}, PhaseFailed: {},
	},
	PhaseActivating: {
		PhaseActive: {}, PhaseDeactivating: {}, PhaseFailed: {},
	},
	PhaseActive: {
		PhaseUninstalled: {}, PhaseDraining: {}, PhaseDeactivating: {}, PhaseFailed: {},
	},
	PhaseDraining: {
		PhaseDeactivating: {}, PhaseActive: {}, PhaseFailed: {},
	},
	PhaseDeactivating: {
		PhaseInstalledInactive: {}, PhaseRemoved: {}, PhaseFailed: {},
	},
	PhaseFailed: {
		PhaseUninstalled: {}, PhaseInstalledInactive: {}, PhaseActivating: {}, PhaseDraining: {},
		PhaseDeactivating: {}, PhaseActive: {}, PhaseRemoved: {}, PhaseFailed: {},
	},
	PhaseRemoved: {
		PhaseUninstalled: {},
	},
}

// Valid 把生命周期值限制在已发布目录内，避免实际态悄悄出现自由字符串。
func (p UnitPhase) Valid() bool {
	_, ok := lifecycleTransitions[p]
	return ok && p != ""
}

// transitionPhase 拒绝设计之外的跳转。所有状态都允许自转换，使重试天然幂等。
func transitionPhase(from, to UnitPhase) error {
	if !to.Valid() {
		return fmt.Errorf("未知生命周期状态 %q", to)
	}
	if from == to {
		return nil
	}
	allowed, ok := lifecycleTransitions[from]
	if !ok {
		return fmt.Errorf("未知当前生命周期状态 %q", from)
	}
	if _, ok := allowed[to]; !ok {
		return fmt.Errorf("非法生命周期转换 %q -> %q", from, to)
	}
	return nil
}
