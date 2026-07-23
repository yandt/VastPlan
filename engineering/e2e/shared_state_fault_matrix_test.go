//go:build e2e

package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/sharedstate"
)

const sharedStateFaultBucket = "VASTPLAN_SHARED_STATE_FAULT_V1"

// TestSharedStateThreeNodeFaultMatrix is a bounded failure matrix, not a soak.
func TestSharedStateThreeNodeFaultMatrix(t *testing.T) {
	cluster := newFaultNATSCluster(t, 3)
	cluster.startAll(t)
	defer cluster.shutdown()

	nc, err := nats.Connect(cluster.servers[0].ClientURL(), nats.MaxReconnects(-1), nats.ReconnectWait(25*time.Millisecond), nats.Timeout(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	kv, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: sharedStateFaultBucket, History: 16, Storage: jetstream.FileStorage,
		Replicas: 3, MaxValueSize: sharedstate.MaxValueBytes, MaxBytes: 64 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sharedstate.NewNATSStore(kv)
	if err != nil {
		t.Fatal(err)
	}
	scope := sharedstate.Scope{
		Kind: sharedstate.ScopeTenant, TenantID: "tenant-a", PluginID: "cn.vastplan.fault-matrix",
		RuntimeScope: "backend-a", Namespace: "state",
	}
	created, err := store.Create(ctx, scope, "active", []byte("v1"))
	if err != nil {
		t.Fatal(err)
	}
	cluster.waitForStreamReplicas(t, "KV_"+sharedStateFaultBucket, 3)
	stoppedLeader, secondStopped := -1, -1
	lastRevision := created.Revision

	t.Run("single_node_loss_keeps_quorum_and_fences_stale_writer", func(t *testing.T) {
		leader := cluster.streamLeader("KV_" + sharedStateFaultBucket)
		if leader < 0 {
			t.Fatal("未找到 Shared State stream leader")
		}
		started := time.Now()
		cluster.stop(leader)
		updated := updateEventually(t, store, scope, "active", []byte("v2"), created.Revision, 12*time.Second)
		t.Logf("A3 evidence single_node_recovery_ms=%d", time.Since(started).Milliseconds())
		if updated.Revision <= created.Revision {
			t.Fatalf("故障接管后 revision 未单调增长: before=%d after=%d", created.Revision, updated.Revision)
		}
		if _, err := store.Update(context.Background(), scope, "active", []byte("stale"), created.Revision); !errors.Is(err, sharedstate.ErrConflict) {
			t.Fatalf("故障接管后旧 revision 必须被拒绝: %v", err)
		}
		stoppedLeader, lastRevision = leader, updated.Revision
	})

	t.Run("quorum_loss_rejects_writes_without_local_fallback", func(t *testing.T) {
		second := cluster.firstRunning()
		if second < 0 {
			t.Fatal("没有可停止的第二个节点")
		}
		cluster.stop(second)
		failureCtx, failureCancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		defer failureCancel()
		if _, err := store.Update(failureCtx, scope, "active", []byte("must-not-commit"), lastRevision); err == nil {
			t.Fatal("失去多数派时 Shared State 写入不得成功或回退本地")
		}
		secondStopped = second
	})

	t.Run("quorum_restore_reconnects_and_preserves_revision", func(t *testing.T) {
		started := time.Now()
		cluster.restart(t, stoppedLeader)
		cluster.restart(t, secondStopped)
		cluster.waitFormed(t)
		waitForCurrentStreamReplicas(t, js, "KV_"+sharedStateFaultBucket, 3)
		entry := getEventually(t, store, scope, "active", 15*time.Second)
		t.Logf("A3 evidence quorum_restore_ms=%d", time.Since(started).Milliseconds())
		if value := string(entry.Value); value != "v2" && value != "must-not-commit" {
			t.Fatalf("仲裁恢复后读取了未知状态: value=%q revision=%d", entry.Value, entry.Revision)
		}
		if entry.Revision < lastRevision {
			t.Fatalf("仲裁恢复后 revision 倒退: got=%d minimum=%d", entry.Revision, lastRevision)
		}
		if string(entry.Value) == "must-not-commit" {
			t.Log("A3 evidence timed_out_write=committed_after_quorum_restore; caller must reconcile before retry")
		}
		updated := updateEventually(t, store, scope, "active", []byte("v3"), entry.Revision, 10*time.Second)
		if updated.Revision <= entry.Revision {
			t.Fatalf("恢复后 revision 未继续增长: before=%d after=%d", entry.Revision, updated.Revision)
		}
		if _, err := store.Update(context.Background(), scope, "active", []byte("stale-after-recovery"), lastRevision); !errors.Is(err, sharedstate.ErrConflict) {
			t.Fatalf("恢复前 writer 不得覆盖恢复后的值: %v", err)
		}
	})
}
