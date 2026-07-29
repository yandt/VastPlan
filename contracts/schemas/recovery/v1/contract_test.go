package recoveryv1

import (
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestNormalizeCapsuleAndCumulativeUnits(t *testing.T) {
	capsule, err := NormalizeCapsule(Capsule{
		Version: Version, ID: "platform", Inventory: InventoryBinding{RepositoryID: "seed", Generation: 7},
		Artifacts: []Artifact{{Ref: pluginv1.ArtifactRef{PluginID: "plugin.a", Version: "1.0.0", Channel: "stable"}, SHA256: strings.Repeat("a", 64)}},
		Stages: []Stage{
			{ID: StageRecovery, Units: []string{"repository"}},
			{ID: StageControlPlane, Units: []string{"deployment"}},
			{ID: StagePlatform, Units: []string{"database"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	units, err := RequiredUnits(capsule.Stages, StageControlPlane)
	if err != nil || strings.Join(units, ",") != "deployment,repository" {
		t.Fatalf("unexpected cumulative units: %v %v", units, err)
	}
}

func TestNormalizePlanRejectsMissingOrDuplicateClassification(t *testing.T) {
	_, err := NormalizePlan(Plan{Version: Version, ID: "platform", Stages: []Stage{
		{ID: StageRecovery, Units: []string{"repository"}},
		{ID: StageControlPlane, Units: []string{"repository"}},
		{ID: StagePlatform, Units: []string{"database"}},
	}})
	if err == nil {
		t.Fatal("duplicate unit classification must fail")
	}
}
