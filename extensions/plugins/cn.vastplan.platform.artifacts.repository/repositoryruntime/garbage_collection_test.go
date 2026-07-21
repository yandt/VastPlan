package repositoryruntime

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/garbagecollection"
)

func TestGarbageCollectionPlansQuarantinesRecoversAndSweeps(t *testing.T) {
	volume, _ := migrationVolumes(t, "repository.unused")
	trust, privateKey := migrationTrust(t)
	statePath := volume.MountPath + ".migration.json"
	manager, err := Open(volume, trust, statePath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, proof, body := migrationArtifact(t, privateKey, "3.0.0")
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	if _, err := manager.Publish(proof, body); err != nil {
		t.Fatal(err)
	}
	publishBootstrapGCHealth(t, manager, 1)
	revision := manager.Stats().Revision
	if _, _, err := manager.SetLifecycle(catalog.LifecycleRequest{Ref: ref, Status: catalog.LifecycleYanked, Reason: "retired by test", ExpectedRevision: revision}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	stale := manager.PlanGarbageCollection(now)
	if !stale.Ready || len(stale.Candidates) != 1 || stale.Candidates[0].SHA256 != artifact.SHA256 || stale.PlanID == "" {
		t.Fatalf("GC plan 未返回精确候选: %+v", stale)
	}
	publishBootstrapGCHealth(t, manager, 2)
	if heartbeat := manager.PlanGarbageCollection(now); heartbeat.PlanID != stale.PlanID || heartbeat.ReferenceRevision == stale.ReferenceRevision {
		t.Fatalf("同保护集合心跳只更新审计 revision，不应令 GC plan 失效: before=%+v after=%+v", stale, heartbeat)
	}
	publishBootstrapGCHealthWithReference(t, manager, 3, artifact)
	if _, err := manager.QuarantineGarbageCollection(stale.PlanID, 24*time.Hour, now); err == nil {
		t.Fatal("候选出现新精确引用后旧 GC plan 必须失效")
	}
	publishBootstrapGCHealth(t, manager, 4)
	plan := manager.PlanGarbageCollection(now)
	status, err := manager.QuarantineGarbageCollection(plan.PlanID, 24*time.Hour, now)
	if err != nil || len(status.Items) != 1 || status.Items[0].Status != garbagecollection.StatusQuarantined {
		t.Fatalf("GC quarantine 失败: status=%+v err=%v", status, err)
	}
	if _, err := manager.Publish(proof, body); err == nil {
		t.Fatal("已隔离不可变 ref 不得通过重发复活")
	}
	if _, _, err := manager.SetLifecycle(catalog.LifecycleRequest{Ref: ref, Status: catalog.LifecycleActive, Reason: "resurrect", ExpectedRevision: manager.Stats().Revision}, now); err == nil {
		t.Fatal("已隔离制品不得恢复为 active")
	}

	restarted, err := Open(volume, trust, statePath)
	if err != nil {
		raw, _ := os.ReadFile(filepath.Join(volume.MountPath, "catalog", "references.json"))
		t.Fatalf("Catalog 必须在隔离制品缺席时保留历史并恢复: %v\nreferences=%s", err, raw)
	}
	if status := restarted.GarbageCollectionStatus(); len(status.Items) != 1 || status.Items[0].Status != garbagecollection.StatusQuarantined {
		t.Fatalf("GC 隔离状态未跨重启恢复: %+v", status)
	}
	if status, err := restarted.SweepGarbageCollection(now.Add(23 * time.Hour)); err != nil || status.Items[0].Status != garbagecollection.StatusQuarantined {
		t.Fatalf("宽限期内不得 sweep: status=%+v err=%v", status, err)
	}
	status, err = restarted.SweepGarbageCollection(now.Add(24 * time.Hour))
	if err != nil || status.Items[0].Status != garbagecollection.StatusSwept || status.Items[0].SweptAt == nil {
		t.Fatalf("宽限期后 sweep 失败: status=%+v err=%v", status, err)
	}
	if _, err := Open(volume, trust, statePath); err != nil {
		t.Fatalf("sweep 后 Catalog 历史必须仍可重建: %v", err)
	}
}

func publishBootstrapGCHealthWithReference(t *testing.T, manager *Manager, generation uint64, artifact pluginv1.Artifact) {
	t.Helper()
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	seed, err := artifactreference.Seal(pluginv1.ArtifactReferenceSnapshot{
		OwnerKind: artifactreference.OwnerSeed, OwnerID: "seed/primary", Generation: generation,
		References: []pluginv1.ArtifactReference{{Ref: ref, SHA256: artifact.SHA256, Purpose: "seed"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.PutReferences("system", "bootstrap-inventory/primary", seed, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	lkg, err := artifactreference.Seal(pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerLastKnownGood, OwnerID: "lkg/primary", Generation: generation, References: []pluginv1.ArtifactReference{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.PutReferences("system", "bootstrap-inventory/primary", lkg, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
}

func TestGarbageCollectionFailsClosedWithoutBootstrapHealth(t *testing.T) {
	volume, _ := migrationVolumes(t, "repository.unused")
	trust, privateKey := migrationTrust(t)
	manager, err := Open(volume, trust, volume.MountPath+".migration.json")
	if err != nil {
		t.Fatal(err)
	}
	artifact, proof, body := migrationArtifact(t, privateKey, "4.0.0")
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	if _, err := manager.Publish(proof, body); err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.SetLifecycle(catalog.LifecycleRequest{Ref: ref, Status: catalog.LifecycleRevoked, Reason: "security revoke", ExpectedRevision: manager.Stats().Revision}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	plan := manager.PlanGarbageCollection(time.Now().UTC())
	if plan.Ready || plan.PlanID != "" || len(plan.Blockers) == 0 {
		t.Fatalf("Seed/LKG 未就绪时 GC 必须 fail-closed: %+v", plan)
	}
}

func publishBootstrapGCHealth(t *testing.T, manager *Manager, generation uint64) {
	t.Helper()
	for _, value := range []struct{ kind, id string }{
		{artifactreference.OwnerSeed, "seed/primary"},
		{artifactreference.OwnerLastKnownGood, "lkg/primary"},
	} {
		snapshot, err := artifactreference.Seal(pluginv1.ArtifactReferenceSnapshot{OwnerKind: value.kind, OwnerID: value.id, Generation: generation, References: []pluginv1.ArtifactReference{}})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := manager.PutReferences("system", "bootstrap-inventory/primary", snapshot, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
}
