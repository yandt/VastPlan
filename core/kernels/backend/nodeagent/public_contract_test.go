package nodeagent_test

import (
	"context"
	"testing"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent/model"
)

var _ nodeagent.Runtime = (*nodeagent.ProtocolRuntime)(nil)

func TestRuntimeFacadePreservesPublicContract(t *testing.T) {
	runtime := nodeagent.NewProtocolRuntime("1.0.0", t.Logf)
	if runtime == nil {
		t.Fatal("NewProtocolRuntime returned nil")
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}

	unit := nodeagent.RuntimeUnit{
		ID: "platform-runtime",
		Plugins: []nodeagent.InstalledPlugin{{
			ID: "cn.vastplan.platform.runtime",
			State: &nodeagent.PluginStateContract{
				PluginStateIdentity: nodeagent.PluginStateIdentity{Format: "runtime", FormatVersion: 1},
			},
		}},
	}
	var neutral model.RuntimeUnit = unit
	var facade nodeagent.RuntimeUnit = neutral
	if facade.ID != unit.ID || facade.Plugins[0].State.Format != "runtime" {
		t.Fatalf("facade model changed: %#v", facade)
	}

	barrier := readinessBarrier(func(context.Context, nodeagent.ReplacementCandidate) error { return nil })
	var _ nodeagent.ReplacementReadinessBarrier = barrier
}

func TestRuntimeFacadePreservesPolicyConstructors(t *testing.T) {
	if _, err := nodeagent.ParseContextPolicy("scope.tenant,caller", "vastplan=*"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeagent.ParseExecutionPolicy("require-isolation", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeagent.ParseRuntimeHostingPolicy("shared", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeagent.ParsePlacementPolicy("process-only", "", ""); err != nil {
		t.Fatal(err)
	}
	if manager := nodeagent.NewRuntimePoolManager(t.Logf); manager == nil {
		t.Fatal("NewRuntimePoolManager returned nil")
	} else if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

type readinessBarrier func(context.Context, nodeagent.ReplacementCandidate) error

func (f readinessBarrier) AwaitReady(ctx context.Context, candidate nodeagent.ReplacementCandidate) error {
	return f(ctx, candidate)
}
