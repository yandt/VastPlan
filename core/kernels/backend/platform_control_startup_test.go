package main

import (
	"context"
	"testing"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

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
