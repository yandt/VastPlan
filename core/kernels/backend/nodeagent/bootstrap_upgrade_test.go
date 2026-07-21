package nodeagent

import (
	"context"
	"strings"
	"testing"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

type recordingBootstrapUpgrade struct {
	inventory bootstrapinventory.Inventory
	events    *[]string
}

func (c *recordingBootstrapUpgrade) Begin([]bootstrapinventory.Item) (bootstrapinventory.Inventory, error) {
	*c.events = append(*c.events, "begin")
	return c.inventory, nil
}

func (c *recordingBootstrapUpgrade) Prepare(_ context.Context, values []VerifiedArtifact) (bootstrapinventory.Inventory, error) {
	*c.events = append(*c.events, "prepare")
	if len(values) == 0 {
		panic("prepare did not receive verified artifacts")
	}
	return c.inventory, nil
}

func (c *recordingBootstrapUpgrade) Commit(context.Context) (bootstrapinventory.Inventory, error) {
	*c.events = append(*c.events, "commit")
	c.inventory.Generation++
	return c.inventory, nil
}

type orderedReferencePublisher struct{ events *[]string }

func (p orderedReferencePublisher) Publish(_ context.Context, _ string, value pluginv1.ArtifactReferenceSnapshot) error {
	event := "assignment"
	if value.OwnerKind == artifactreference.OwnerSeed || value.OwnerKind == artifactreference.OwnerLastKnownGood {
		event = "bootstrap"
	}
	*p.events = append(*p.events, event)
	return nil
}

func testBootstrapInventory(t *testing.T) bootstrapinventory.Inventory {
	t.Helper()
	item := bootstrapinventory.Item{
		Ref:    pluginv1.ArtifactRef{PluginID: "cn.vastplan.platform.artifacts.repository", Version: "1.0.0", Channel: "stable"},
		SHA256: strings.Repeat("1", 64),
	}
	value, err := bootstrapinventory.Normalize(bootstrapinventory.Inventory{
		Version: bootstrapinventory.Version, Generation: 1, RepositoryID: "test-seed",
		Seed: []bootstrapinventory.Item{item}, LastKnownGood: []bootstrapinventory.Item{item},
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestReconcileCommitsBootstrapOnlyAfterAssignmentPublication(t *testing.T) {
	events := []string{}
	inventory := testBootstrapInventory(t)
	coordinator := &recordingBootstrapUpgrade{inventory: inventory, events: &events}
	runtime := newFakeRuntime()
	reconciler := newTestReconciler(runtime, NewMemoryStateStore())
	reconciler.BootstrapInventory = &inventory
	reconciler.BootstrapUpgrade = coordinator
	reconciler.References = orderedReferencePublisher{events: &events}
	reconciler.BootstrapReferences = orderedReferencePublisher{events: &events}
	state := desired(1, "1.0.0", true)
	state.Metadata.Tenant = "system"

	result, err := reconciler.Reconcile(context.Background(), state)
	if err != nil || !result.Converged {
		t.Fatalf("reconcile failed: %+v err=%v", result, err)
	}
	want := []string{"begin", "prepare", "assignment", "commit", "bootstrap", "bootstrap"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("Bootstrap 事务顺序错误: got=%v want=%v", events, want)
	}
	if result.State.BootstrapGeneration != 2 {
		t.Fatalf("发布的 Bootstrap generation 不是 Commit 后版本: %+v", result.State)
	}
}

func TestReconcileFailedRuntimeNeverCommitsBootstrapLKG(t *testing.T) {
	events := []string{}
	inventory := testBootstrapInventory(t)
	coordinator := &recordingBootstrapUpgrade{inventory: inventory, events: &events}
	runtime := newFakeRuntime()
	runtime.failEntry = "/2.0.0"
	reconciler := newTestReconciler(runtime, NewMemoryStateStore())
	reconciler.BootstrapInventory = &inventory
	reconciler.BootstrapUpgrade = coordinator
	reconciler.BootstrapReferences = orderedReferencePublisher{events: &events}

	state := desired(2, "2.0.0", true)
	state.Metadata = deploymentv1.Metadata{Name: "test", Tenant: "system"}
	result, err := reconciler.Reconcile(context.Background(), state)
	if err == nil || result.Converged {
		t.Fatalf("运行时失败必须阻止收敛: %+v err=%v", result, err)
	}
	if strings.Contains(strings.Join(events, ","), "commit") {
		t.Fatalf("运行时失败后不得推进 LKG: %v", events)
	}
}
