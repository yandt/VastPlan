package pluginv1

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArtifactPlanningProjectionRejectsIdentityDrift(t *testing.T) {
	manifest := manifestFixture("cn.vastplan.product.planning", "1.0.0")
	response := ArtifactPlanningResponse{RepositoryRevision: 7, Items: []ArtifactPlanningDescriptor{{
		Ref:    ArtifactRef{PluginID: "cn.vastplan.product.planning", Version: "1.0.0", Channel: "stable"},
		SHA256: strings.Repeat("a", 64), Publisher: "vastplan", Manifest: manifest,
	}}}
	if _, err := ValidateArtifactPlanningResponse(response); err != nil {
		t.Fatal(err)
	}
	response.Items[0].Publisher = "attacker"
	if _, err := ValidateArtifactPlanningResponse(response); err == nil {
		t.Fatal("仓库规划投影不得接受与 Manifest 不一致的发布者")
	}
}

func TestArtifactPlanningRequestIsClosedAndDeterministic(t *testing.T) {
	raw, _ := json.Marshal(ArtifactPlanningRequest{Refs: []ArtifactRef{
		{PluginID: "cn.vastplan.product.z", Version: "1.0.0", Channel: "stable"},
		{PluginID: "cn.vastplan.product.a", Version: "1.0.0", Channel: "stable"},
	}})
	request, err := ParseArtifactPlanningRequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if request.Refs[0].PluginID != "cn.vastplan.product.a" {
		t.Fatalf("规划描述请求未规范排序: %+v", request.Refs)
	}
	if _, err := ParseArtifactPlanningRequest([]byte(`{"refs":[],"path":"/private/repository"}`)); err == nil {
		t.Fatal("规划描述请求必须拒绝仓库路径等未知字段")
	}
}

func manifestFixture(id, version string) json.RawMessage {
	raw, _ := json.Marshal(Manifest{
		ID: id, Name: "planning", Description: "planning", Version: version, Publisher: "vastplan",
		Engines: map[string]string{"backend": "^0.1"}, Activation: []string{"onStartup"},
		Entry: map[string]string{"backend": "backend/main"}, Contributes: map[string]json.RawMessage{"backend": []byte(`{"tools":[]}`)},
	})
	return raw
}
