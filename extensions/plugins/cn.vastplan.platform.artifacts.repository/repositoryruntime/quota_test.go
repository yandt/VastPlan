package repositoryruntime

import (
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
)

func TestQuotaAdmissionAndCapacityFollowPhysicalRetirement(t *testing.T) {
	volume, _ := migrationVolumes(t, "repository.unused")
	trust, privateKey := migrationTrust(t)
	manager, err := Open(volume, trust, volume.MountPath+".migration.json", Options{Quota: QuotaPolicy{
		QuotaLimit: QuotaLimit{MaxArtifacts: 1},
		Rules:      []QuotaRule{{ID: "testing-bytes", Channel: "testing", QuotaLimit: QuotaLimit{MaxBytes: 1 << 20}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	first, proof, body := migrationArtifact(t, privateKey, "5.0.0")
	if _, err := manager.Publish(proof, body); err != nil {
		t.Fatal(err)
	}
	_, secondProof, secondBody := migrationArtifact(t, privateKey, "5.1.0")
	if _, err := manager.Publish(secondProof, secondBody); err == nil {
		t.Fatal("全局 artifact 配额必须在写入前拒绝第二个制品")
	}
	capacity := manager.Capacity()
	if capacity.ActiveArtifacts != 1 || capacity.ActiveBytes != first.Size || capacity.StoredBytes != first.Size || len(capacity.Quotas) != 2 || capacity.Quotas[0].Artifacts != 1 {
		t.Fatalf("活动容量或配额用量不正确: %+v", capacity)
	}

	ref := pluginv1.ArtifactRef{PluginID: first.PluginID, Version: first.Version, Channel: first.Channel}
	if _, _, err := manager.SetLifecycle(catalog.LifecycleRequest{Ref: ref, Status: catalog.LifecycleYanked, Reason: "quota retirement", ExpectedRevision: manager.Stats().Revision}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	publishBootstrapGCHealth(t, manager, 1)
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	plan := manager.PlanGarbageCollection(now)
	if _, err := manager.QuarantineGarbageCollection(plan.PlanID, 24*time.Hour, now); err != nil {
		t.Fatal(err)
	}
	capacity = manager.Capacity()
	if capacity.ActiveArtifacts != 0 || capacity.QuarantinedArtifacts != 1 || capacity.QuarantinedBytes != first.Size || capacity.StoredBytes != first.Size {
		t.Fatalf("隔离容量未从活动配额分离: %+v", capacity)
	}
	if _, err := manager.Publish(secondProof, secondBody); err != nil {
		t.Fatalf("隔离对象不应继续占用活动发布配额: %v", err)
	}
	if _, err := manager.SweepGarbageCollection(now.Add(24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	capacity = manager.Capacity()
	if capacity.ActiveArtifacts != 1 || capacity.QuarantinedArtifacts != 0 || capacity.SweptArtifacts != 1 || capacity.ReclaimedBytes != first.Size || capacity.StoredBytes != capacity.ActiveBytes {
		t.Fatalf("sweep 后容量统计不正确: %+v", capacity)
	}
}

func TestQuotaPolicyRejectsAmbiguousOrEmptyRules(t *testing.T) {
	for _, policy := range []QuotaPolicy{
		{Rules: []QuotaRule{{ID: "empty"}}},
		{Rules: []QuotaRule{{ID: "same", Channel: "testing", QuotaLimit: QuotaLimit{MaxArtifacts: 1}}, {ID: "same", Publisher: "example", QuotaLimit: QuotaLimit{MaxArtifacts: 2}}}},
		{Rules: []QuotaRule{{ID: "Bad", Channel: "testing", QuotaLimit: QuotaLimit{MaxArtifacts: 1}}}},
	} {
		if err := policy.Validate(); err == nil {
			t.Fatalf("非法配额策略必须被拒绝: %+v", policy)
		}
	}
}
