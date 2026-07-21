package edge

import (
	"context"
	"sort"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

// RunFrontendPrefetcher continuously projects current Portal Activations from the
// governed Composer state into this Portal Edge node's local delivery cache.
// The tenant list comes from the platform-owned catalog, never from a browser.
func RunFrontendPrefetcher(ctx context.Context, service portalapi.Service, catalog *TrustedCatalog, updates *PortalUpdateHub, tenantIDs []string, interval time.Duration, logf func(string, ...any)) {
	if service == nil || catalog == nil || interval <= 0 {
		return
	}
	tenants := uniqueTenantIDs(tenantIDs)
	if len(tenants) == 0 {
		return
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	current := map[string]portalapi.PortalSpec{}
	sync := func() {
		for _, tenantID := range tenants {
			principal := portalapi.Principal{ID: "portal-edge-prefetch", TenantID: tenantID, System: true}
			activations, err := service.ListActivations(ctx, principal)
			if err != nil {
				logf("Portal Edge 预取无法读取 tenant=%s 的当前 Activation: %v", tenantID, err)
				continue
			}
			for _, activation := range activations {
				if !isCurrentActivation(activation, tenantID) {
					continue
				}
				if err := catalog.PrefetchPortal(ctx, tenantID, activation.Spec); err != nil {
					logf("Portal Edge 预取失败 tenant=%s portal=%s activation=%d: %v", tenantID, activation.PortalID, activation.ID, err)
					continue
				}
				key := tenantID + "\x00" + activation.PortalID
				previous, exists := current[key]
				current[key] = activation.Spec
				if exists && previous.Revision != activation.Spec.Revision {
					updates.Publish(PortalUpdate{TenantID: tenantID, PortalID: activation.PortalID, Activation: activation.ID, Mode: classifyPortalUpdate(previous, activation.Spec)})
				}
			}
		}
	}
	sync()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sync()
		}
	}
}

func uniqueTenantIDs(values []string) []string {
	seen := map[string]struct{}{}
	for _, value := range values {
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
