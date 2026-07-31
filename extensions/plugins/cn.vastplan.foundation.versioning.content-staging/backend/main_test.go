package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	staging "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.content-staging/contentstaging"
)

func TestRuntimeDescriptorAndVersionMatchSignedManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "vastplan.plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(contributions) != 3 {
		t.Fatalf("manifest contribution 无效: %+v %v", contributions, err)
	}
	service := staging.NewService(nil)
	runtimeContributions := map[string][]byte{
		stagingv1.Capability:                service.Contribution().Descriptor,
		contentv1.Capability:                service.ContentReferenceContribution().Descriptor,
		stagingv1.UploadDataPlaneCapability: uploadDataPlaneContribution(nil).Descriptor,
	}
	for _, contribution := range contributions {
		var signed, runtime any
		descriptor, ok := runtimeContributions[contribution.ID]
		if !ok || json.Unmarshal(contribution.Descriptor, &signed) != nil || json.Unmarshal(descriptor, &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
			t.Fatalf("运行时 descriptor 与签名 Manifest 不一致\nid=%s\nsigned=%s\nruntime=%s", contribution.ID, contribution.Descriptor, descriptor)
		}
	}
	if manifest.Version != pluginVersion || pluginVersion != staging.PluginVersion {
		t.Fatalf("插件版本未使用 Manifest 单一真相源: manifest=%s main=%s package=%s", manifest.Version, pluginVersion, staging.PluginVersion)
	}
}
