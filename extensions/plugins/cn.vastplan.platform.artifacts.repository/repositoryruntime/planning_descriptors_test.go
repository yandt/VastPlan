package repositoryruntime

import (
	"encoding/json"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestDescribePlanningReturnsVerifiedManifestWithoutPackageMaterial(t *testing.T) {
	volume, _ := migrationVolumes(t, "repository.planning")
	trust, privateKey := migrationTrust(t)
	manager, err := Open(volume, trust, filepath.Join(t.TempDir(), "state", "migration.json"), Options{SupplyChain: SupplyChainPolicy{RequiredSBOMChannels: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	artifact, proof, body := migrationArtifact(t, privateKey, "8.0.0")
	if _, err := manager.Publish(proof, body); err != nil {
		t.Fatal(err)
	}
	requestRaw, _ := json.Marshal(pluginv1.ArtifactPlanningRequest{Refs: []pluginv1.ArtifactRef{{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}}})
	request, err := pluginv1.ParseArtifactPlanningRequest(requestRaw)
	if err != nil {
		t.Fatal(err)
	}
	response, err := manager.DescribePlanning(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.RepositoryRevision == 0 || len(response.Items) != 1 || response.Items[0].SHA256 != artifact.SHA256 || len(response.Items[0].Manifest) == 0 {
		t.Fatalf("制品规划描述不完整: %+v", response)
	}
}
