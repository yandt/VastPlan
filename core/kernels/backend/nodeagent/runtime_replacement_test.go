package nodeagent

import (
	"context"
	"errors"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type observingReplacementBarrier struct {
	runtime   *ProtocolRuntime
	expected  *runningUnit
	candidate ReplacementCandidate
	err       error
	sawOld    bool
}

func (b *observingReplacementBarrier) AwaitReady(_ context.Context, candidate ReplacementCandidate) error {
	b.candidate = candidate
	b.runtime.mu.RLock()
	b.sawOld = b.runtime.units[candidate.UnitID] == b.expected
	b.runtime.mu.RUnlock()
	return b.err
}

func TestApplyTransactionWaitsForReplacementBarrierBeforeCommit(t *testing.T) {
	runtime := NewProtocolRuntime("1.0.0", nil)
	old := &runningUnit{fingerprint: "old"}
	runtime.units["database"] = old
	barrier := &observingReplacementBarrier{runtime: runtime, expected: old}
	runtime.ReplacementReadiness = barrier
	transaction := &applyTransaction{
		runtime:   runtime,
		unit:      RuntimeUnit{ID: "database", Fingerprint: "candidate", StartupTier: "bootstrap"},
		instances: []*protocolbus.PluginInstance{{RuntimeAudience: "runtime-candidate"}},
	}

	retired, current, err := transaction.activateAndCommit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !barrier.sawOld || retired != old || runtime.units["database"] != current {
		t.Fatalf("候选屏障前后 generation 顺序错误: sawOld=%v retired=%p old=%p", barrier.sawOld, retired, old)
	}
	if !barrier.candidate.Replacing || barrier.candidate.StartupTier != "bootstrap" || len(barrier.candidate.RuntimeInstanceIDs) != 1 || barrier.candidate.RuntimeInstanceIDs[0] != "runtime-candidate" {
		t.Fatalf("候选屏障证据不完整: %+v", barrier.candidate)
	}
}

func TestReplacementBarrierFailureKeepsOldGeneration(t *testing.T) {
	runtime := NewProtocolRuntime("1.0.0", nil)
	old := &runningUnit{fingerprint: "old"}
	runtime.units["database"] = old
	barrier := &observingReplacementBarrier{runtime: runtime, expected: old, err: errors.New("candidate not opened")}
	runtime.ReplacementReadiness = barrier
	transaction := &applyTransaction{
		runtime:   runtime,
		unit:      RuntimeUnit{ID: "database", Fingerprint: "candidate", StartupTier: "bootstrap"},
		instances: []*protocolbus.PluginInstance{{RuntimeAudience: "runtime-candidate"}},
	}

	if _, _, err := transaction.activateAndCommit(context.Background()); err == nil {
		t.Fatal("候选 Open 失败必须阻止 generation 提交")
	}
	if runtime.units["database"] != old || transaction.committed {
		t.Fatal("屏障失败后旧代必须保持活动")
	}
}

var _ ReplacementReadinessBarrier = (*observingReplacementBarrier)(nil)
