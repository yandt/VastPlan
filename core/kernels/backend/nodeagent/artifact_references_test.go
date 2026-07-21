package nodeagent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type recordingAssignmentReferences struct {
	values []pluginv1.ArtifactReferenceSnapshot
	err    error
}

func (p *recordingAssignmentReferences) Publish(_ context.Context, _ string, value pluginv1.ArtifactReferenceSnapshot) error {
	p.values = append(p.values, value)
	return p.err
}

type referenceInstaller struct{ fakeInstaller }

func (referenceInstaller) Install(verified VerifiedArtifact) (InstalledPlugin, error) {
	plugin, err := (fakeInstaller{}).Install(verified)
	plugin.SHA256 = strings.Repeat("a", 64)
	return plugin, err
}

func TestAssignmentReferencesHeartbeatRetryAndGracefulRelease(t *testing.T) {
	runtime := newFakeRuntime()
	store := NewMemoryStateStore()
	publisher := &recordingAssignmentReferences{}
	now := time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC)
	reconciler := newTestReconciler(runtime, store)
	reconciler.Installer = referenceInstaller{}
	reconciler.References = publisher
	reconciler.RequireArtifactReferences = true
	reconciler.Now = func() time.Time { return now }
	want := desired(7, "1.0.0", true)
	want.Metadata.Tenant = "tenant-a"

	first, err := reconciler.Reconcile(context.Background(), want)
	if err != nil || !first.Converged || first.State.ReferencePending || first.State.ReferenceGeneration != 1 || len(publisher.values) != 1 {
		t.Fatalf("首次 Assignment 引用发布失败: result=%+v snapshots=%+v err=%v", first, publisher.values, err)
	}
	snapshot := publisher.values[0]
	if snapshot.OwnerKind != "assignment-active" || snapshot.OwnerID != "assignment/test/node-1" || snapshot.TTLSeconds != 120 || len(snapshot.References) != 1 || snapshot.References[0].SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("Assignment 快照未绑定实际安装制品: %+v", snapshot)
	}
	if _, err := reconciler.Reconcile(context.Background(), want); err != nil || len(publisher.values) != 1 {
		t.Fatalf("心跳期限内不应重复写仓库: snapshots=%d err=%v", len(publisher.values), err)
	}
	now = now.Add(41 * time.Second)
	if _, err := reconciler.Reconcile(context.Background(), want); err != nil || len(publisher.values) != 2 || publisher.values[1].Generation != 2 {
		t.Fatalf("Assignment 心跳未推进 generation: %+v err=%v", publisher.values, err)
	}

	publisher.err = errors.New("repository unavailable")
	now = now.Add(41 * time.Second)
	failed, err := reconciler.Reconcile(context.Background(), want)
	if err == nil || !failed.State.ReferencePending || failed.State.ReferenceGeneration != 3 {
		t.Fatalf("仓库失败必须保留持久 outbox: result=%+v err=%v", failed, err)
	}
	failedDigest := publisher.values[len(publisher.values)-1].Digest
	publisher.err = nil
	retried, err := reconciler.Reconcile(context.Background(), want)
	if err != nil || retried.State.ReferencePending || publisher.values[len(publisher.values)-1].Generation != 3 || publisher.values[len(publisher.values)-1].Digest != failedDigest {
		t.Fatalf("outbox 必须以同 generation/同 digest 幂等重试: result=%+v snapshots=%+v err=%v", retried, publisher.values, err)
	}

	if err := reconciler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	released := publisher.values[len(publisher.values)-1]
	if released.Generation != 4 || released.TTLSeconds != 0 || len(released.References) != 0 {
		t.Fatalf("优雅停机必须发布永久健康的空快照: %+v", released)
	}
}

func TestManagedArtifactSourceRequiresReferencePublisher(t *testing.T) {
	reconciler := newTestReconciler(newFakeRuntime(), NewMemoryStateStore())
	reconciler.RequireArtifactReferences = true
	if _, err := reconciler.Reconcile(context.Background(), desired(1, "1.0.0", true)); err == nil {
		t.Fatal("托管制品源缺少 Assignment 引用发布器必须 fail-closed")
	}
}
