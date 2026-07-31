package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
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
	if err != nil || len(contributions) != 1 {
		t.Fatalf("manifest contribution 无效: %+v %v", contributions, err)
	}
	service := staging.NewService(nil)
	var signed, runtime any
	if json.Unmarshal(contributions[0].Descriptor, &signed) != nil || json.Unmarshal(service.Contribution().Descriptor, &runtime) != nil || !reflect.DeepEqual(signed, runtime) {
		t.Fatalf("运行时 descriptor 与签名 Manifest 不一致\nsigned=%s\nruntime=%s", contributions[0].Descriptor, service.Contribution().Descriptor)
	}
	if manifest.Version != pluginVersion || pluginVersion != staging.PluginVersion {
		t.Fatalf("插件版本未使用 Manifest 单一真相源: manifest=%s main=%s package=%s", manifest.Version, pluginVersion, staging.PluginVersion)
	}
}
