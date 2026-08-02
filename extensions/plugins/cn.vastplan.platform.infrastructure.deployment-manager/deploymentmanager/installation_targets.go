package deploymentmanager

import (
	"sort"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

func (s *Service) ListPluginInstallationTargets(call *contractv1.CallContext) ([]plugininstallation.TargetOption, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]plugininstallation.TargetOption, 0)
	for _, revision := range s.tenantLocked(tenant).Revisions {
		if !revision.Active || revision.Status != platformadminapi.ServicePublished || revision.Intent == nil {
			continue
		}
		for _, service := range revision.Intent.Services {
			if !strings.HasPrefix(service.ServiceClass, "application.") {
				continue
			}
			items = append(items, plugininstallation.TargetOption{
				Target:       plugininstallation.Target{Kernel: "backend", Deployment: revision.Deployment, UnitID: service.ID},
				ServiceClass: service.ServiceClass, ActiveRevision: revision.ID,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Target.Deployment != items[j].Target.Deployment {
			return items[i].Target.Deployment < items[j].Target.Deployment
		}
		return items[i].Target.UnitID < items[j].Target.UnitID
	})
	return items, nil
}
