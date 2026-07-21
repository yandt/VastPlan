package artifactreference

import (
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestSealProducesCanonicalValidatedSnapshot(t *testing.T) {
	snapshot, err := Seal(pluginv1.ArtifactReferenceSnapshot{OwnerKind: OwnerDeploymentActive, OwnerID: "platform/api", Generation: 3, References: []pluginv1.ArtifactReference{
		{Ref: pluginv1.ArtifactRef{PluginID: "cn.example.worker", Version: "1.2.0", Channel: "stable"}, SHA256: strings.Repeat("b", 64), Purpose: "active"},
		{Ref: pluginv1.ArtifactRef{PluginID: "cn.example.api", Version: "1.0.0", Channel: "stable"}, SHA256: strings.Repeat("a", 64), Purpose: "active"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != SchemaVersion || len(snapshot.Digest) != 64 || snapshot.References[0].Ref.PluginID != "cn.example.api" {
		t.Fatalf("snapshot was not canonicalized: %+v", snapshot)
	}
	if _, err := SnapshotKey("tenant-a", snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsMutationDuplicateAndInvalidLease(t *testing.T) {
	base, err := Seal(pluginv1.ArtifactReferenceSnapshot{OwnerKind: OwnerAssignmentActive, OwnerID: "deployment/node-a", Generation: 1, TTLSeconds: 60, References: []pluginv1.ArtifactReference{{Ref: pluginv1.ArtifactRef{PluginID: "cn.example.api", Version: "1.0.0", Channel: "stable"}, SHA256: strings.Repeat("a", 64), Purpose: "active"}}})
	if err != nil {
		t.Fatal(err)
	}
	mutated := base
	mutated.References[0].Purpose = "rollback"
	if err := Validate(mutated); err == nil {
		t.Fatal("mutated snapshot must fail digest validation")
	}
	duplicate := base
	duplicate.References = append(duplicate.References, duplicate.References[0])
	if _, err := Seal(duplicate); err == nil {
		t.Fatal("duplicate references must be rejected")
	}
	invalidTTL := base
	invalidTTL.TTLSeconds = 1
	if _, err := Seal(invalidTTL); err == nil {
		t.Fatal("short lease must be rejected")
	}
}
