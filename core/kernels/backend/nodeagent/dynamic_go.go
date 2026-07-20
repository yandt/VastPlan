package nodeagent

import (
	"fmt"

	"cdsoft.com.cn/VastPlan/core/shared/go/pluginid"
)

// validateDynamicGoFirstParty is the Backend-side trust gate. Actual .so
// loading lives exclusively under core/runtimehosts/go-dynamic/loader.
func validateDynamicGoFirstParty(plugin InstalledPlugin) error {
	if plugin.Publisher != pluginid.FirstPartyPublisher {
		return fmt.Errorf("dynamic-go 只允许 publisher=%s，实际为 %s", pluginid.FirstPartyPublisher, plugin.Publisher)
	}
	if _, err := pluginid.ParseFirstParty(plugin.ID); err != nil {
		return fmt.Errorf("dynamic-go 只允许已分类的首方插件: %w", err)
	}
	return nil
}
