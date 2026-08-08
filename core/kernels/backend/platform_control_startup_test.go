package main

import (
	"context"
	"errors"
	"testing"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

type replacementReadinessStub struct {
	instanceIDs []string
	relevant    bool
	err         error
}

func (s *replacementReadinessStub) AwaitCandidateOpen(_ context.Context, instanceIDs []string) (bool, error) {
	s.instanceIDs = append([]string(nil), instanceIDs...)
	return s.relevant, s.err
}

func TestPlatformControlActivationGateAllowsOnlyBootstrapUntilBinding(t *testing.T) {
	binding := sharedstate.NewBindingStore()
	coordinator := &platformControlCoordinator{binding: binding}
	if err := coordinator.Allow(context.Background(), nodeagent.RuntimeUnit{ID: "database-runtime", StartupTier: "bootstrap"}); err != nil {
		t.Fatalf("Bootstrap unit 应立即允许: %v", err)
	}
	if err := coordinator.Allow(context.Background(), nodeagent.RuntimeUnit{ID: "settings", StartupTier: "full"}); err != errPlatformControlNotReady {
		t.Fatalf("Full unit 应被门控: %v", err)
	}
	store, err := sharedstate.OpenFileStore(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.Bind(1, "profile", store); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Allow(context.Background(), nodeagent.RuntimeUnit{ID: "settings", StartupTier: "full"}); err != nil {
		t.Fatalf("Store Ready 后 Full unit 应放行: %v", err)
	}
}

func TestPlatformControlReplacementBarrierAppliesOnlyToBootstrapReplacement(t *testing.T) {
	stub := &replacementReadinessStub{relevant: true}
	coordinator := &platformControlCoordinator{replacement: stub}
	candidate := nodeagent.ReplacementCandidate{
		UnitID: "database-runtime", StartupTier: "bootstrap", Replacing: true,
		RuntimeInstanceIDs: []string{"candidate-a"},
	}
	if err := coordinator.AwaitReady(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if len(stub.instanceIDs) != 1 || stub.instanceIDs[0] != "candidate-a" {
		t.Fatalf("候选 runtime 证据未传入 Platform Control: %v", stub.instanceIDs)
	}

	stub.instanceIDs = nil
	candidate.Replacing = false
	if err := coordinator.AwaitReady(context.Background(), candidate); err != nil || stub.instanceIDs != nil {
		t.Fatalf("冷启动不得等待 replacement Open: ids=%v err=%v", stub.instanceIDs, err)
	}
	candidate.Replacing = true
	candidate.StartupTier = "full"
	if err := coordinator.AwaitReady(context.Background(), candidate); err != nil || stub.instanceIDs != nil {
		t.Fatalf("Full 单元不得进入 Platform Control replacement 屏障: ids=%v err=%v", stub.instanceIDs, err)
	}
}

func TestPlatformControlReplacementBarrierPropagatesOpenFailure(t *testing.T) {
	stub := &replacementReadinessStub{relevant: true, err: errors.New("open failed")}
	coordinator := &platformControlCoordinator{replacement: stub}
	err := coordinator.AwaitReady(context.Background(), nodeagent.ReplacementCandidate{
		StartupTier: "bootstrap", Replacing: true, RuntimeInstanceIDs: []string{"candidate-a"},
	})
	if err == nil || !errors.Is(err, stub.err) {
		t.Fatalf("Open 失败必须阻止旧代退役: %v", err)
	}
}
