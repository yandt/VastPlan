package nodeagent

import (
	"fmt"
	"strings"
)

// backgroundServiceTenant binds autonomous calls to a tenant selected by the
// trusted service configuration. The plugin process can neither choose nor
// widen this identity through CallContext.
func backgroundServiceTenant(plugin InstalledPlugin, values map[string]any) (string, error) {
	if !plugin.Contract.BackgroundService {
		return "", nil
	}
	raw, exists := values["tenantId"]
	tenantID, ok := raw.(string)
	if !exists || !ok || tenantID == "" || strings.TrimSpace(tenantID) != tenantID || len(tenantID) > 160 {
		return "", fmt.Errorf("后台服务插件 %s 要求配置规范的 tenantId", plugin.ID)
	}
	return tenantID, nil
}
