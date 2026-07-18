package edge

import (
	"context"
	"sort"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

// RunFrontendPrefetcher continuously projects active Portal revisions from the
// governed Composer state into this Portal Edge node's local delivery cache.
// The tenant list comes from the platform-owned catalog, never from a browser.
func RunFrontendPrefetcher(ctx context.Context, service portalapi.Service, catalog *TrustedCatalog, tenantIDs []string, interval time.Duration, logf func(string, ...any)) {
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
	sync := func() {
		for _, tenantID := range tenants {
			principal := portalapi.Principal{ID: "portal-edge-prefetch", TenantID: tenantID, System: true}
			revisions, err := service.List(ctx, principal)
			if err != nil {
				logf("Portal Edge 预取无法读取 tenant=%s 的活动 revision: %v", tenantID, err)
				continue
			}
			for _, revision := range revisions {
				if !isActiveRevision(revision, tenantID) {
					continue
				}
				if err := catalog.PrefetchPortal(ctx, tenantID, revision.Spec); err != nil {
					logf("Portal Edge 预取失败 tenant=%s portal=%s revision=%d: %v", tenantID, revision.PortalID, revision.ID, err)
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
