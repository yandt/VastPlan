package arch

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
)

func TestPlatformManagementProfileKernelServiceGrantsMatchSignedRequests(t *testing.T) {
	root := repoRoot(t)
	profile, err := backendcompositionv1.ParsePlatformProfileFile(filepath.Join(root, "engineering", "deploy", "platform-management-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, unit := range profile.Services {
		pluginIDs := make([]string, 0, len(unit.Plugins))
		for _, ref := range unit.Plugins {
			pluginIDs = append(pluginIDs, ref.ID)
		}
		envelope, err := pluginconfig.Parse(unit.Config, pluginIDs)
		if err != nil {
			t.Fatalf("解析平台服务 %s 配置信封: %v", unit.ID, err)
		}
		for _, ref := range unit.Plugins {
			raw, err := os.ReadFile(filepath.Join(root, "extensions", "plugins", ref.ID, "vastplan.plugin.json"))
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := pluginv1.ParseManifest(raw)
			if err != nil {
				t.Fatalf("解析插件 %s: %v", ref.ID, err)
			}
			var requested []string
			if manifest.Capabilities != nil {
				requested = append(requested, manifest.Capabilities.KernelServices...)
			}
			sort.Strings(requested)
			granted := envelope.KernelServiceGrants[ref.ID]
			if !reflect.DeepEqual(requested, granted) && !(len(requested) == 0 && len(granted) == 0) {
				t.Errorf("平台服务 %s 的插件 %s Kernel Service Grant 与 Manifest 申请不一致: requested=%v granted=%v", unit.ID, ref.ID, requested, granted)
			}
		}
	}
}
