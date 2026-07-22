package authorizationpolicy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
)

func TestContributionDescriptorMatchesSignedManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for _, contribution := range contributions {
		if contribution.ExtensionPoint != extpoint.ToolPackage || contribution.ID != Capability {
			continue
		}
		var signed, runtime any
		if err := json.Unmarshal(contribution.Descriptor, &signed); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(contributionDescriptor(), &runtime); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(signed, runtime) {
			t.Fatalf("Authorization Policy descriptor 漂移: signed=%v runtime=%v", signed, runtime)
		}
		return
	}
	t.Fatal("Manifest 缺少 Authorization Policy tool contribution")
}
