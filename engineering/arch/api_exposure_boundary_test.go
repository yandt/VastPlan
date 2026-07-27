package arch

import (
	"encoding/json"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

// api.route 仍属于 Backend v1 兼容面，但产品插件必须使用治理式 apiContracts。
// 这条门禁防止旧公开路径模型重新进入 Node Gateway。
func TestProductPluginsDoNotUseDeprecatedAPIRoutes(t *testing.T) {
	for _, root := range []string{"extensions/plugins", "examples/plugins"} {
		forEachPluginManifest(t, root, func(_ string, parsed pluginv1.Manifest) {
			var backend map[string]json.RawMessage
			if err := json.Unmarshal(parsed.Contributes["backend"], &backend); err != nil && len(parsed.Contributes["backend"]) != 0 {
				t.Errorf("解析 %s Backend 贡献: %v", parsed.ID, err)
				return
			}
			if _, deprecated := backend["apiRoutes"]; deprecated {
				t.Errorf("插件 %s 使用已废弃 apiRoutes；应声明 apiContracts 并由 ApiExposure 发布", parsed.ID)
			}
		})
	}
}
