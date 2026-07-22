// Package main starts the trusted Authorization Policy service.
package main

import (
	"log"
	"os"
	"strings"
	"time"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	policy "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.authorization-policy/authorizationpolicy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	catalog, err := policy.LoadPermissionCatalog(os.Getenv("VASTPLAN_AUTHORIZATION_PERMISSION_CATALOG"))
	if err != nil {
		log.Fatalf("加载 Permission Catalog: %v", err)
	}
	profile := policy.NativeProviderProfile(catalog)
	root, err := policy.RootDomain(catalog, profile)
	if err != nil {
		log.Fatalf("构建 Authorization root domain: %v", err)
	}
	signer, err := policy.LoadSigner(os.Getenv("VASTPLAN_AUTHORIZATION_POLICY_KEY"))
	if err != nil {
		log.Fatalf("加载 Authorization Policy key: %v", err)
	}
	service, err := policy.NewService(policy.ServiceOptions{
		Store: &policy.FileStore{Path: os.Getenv("VASTPLAN_AUTHORIZATION_POLICY_STATE")}, Signer: signer,
		SnapshotWriter: policy.FileSnapshotWriter{Path: os.Getenv("VASTPLAN_AUTHORIZATION_POLICY_SNAPSHOT")},
		Catalog:        catalog, ProviderProfile: profile, Domains: []authorizationv1.PolicyDomain{root},
		DefaultAudience: splitAudience(os.Getenv("VASTPLAN_AUTHORIZATION_POLICY_AUDIENCE")), DefaultTTL: 5 * time.Minute,
	})
	if err != nil {
		log.Fatalf("初始化 Authorization Policy: %v", err)
	}
	plugin := sdk.New(policy.PluginID, policy.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Authorization Policy 退出: %v", err)
	}
}

func splitAudience(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
