package authorizationpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sort"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func LoadPermissionCatalog(path string) (pluginv1.PermissionCatalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return pluginv1.PermissionCatalog{}, err
	}
	return pluginv1.ParsePermissionCatalog(raw)
}

func NativeProviderProfile(catalog pluginv1.PermissionCatalog) authorizationv1.ProviderProfile {
	configDigest := sha256.Sum256([]byte("vastplan.authorization.native.v1\n" + catalog.Digest))
	configuration := authorizationv1.ConfigurationRevisionRef{ProfileID: "authorization.native", Revision: 1, Digest: hex.EncodeToString(configDigest[:])}
	return authorizationv1.ProviderProfile{
		ID: "authorization.native", Revision: 1,
		Store:    authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolStore, ProviderID: "native-file", PluginID: PluginID, Capability: Capability + ".store", Version: PluginVersion, Configuration: configuration},
		Engine:   authorizationv1.ProviderRef{Protocol: authorizationv1.ProtocolEngine, ProviderID: "native-rbac", PluginID: "cn.vastplan.foundation.security.authorization-engine.native", Capability: "foundation.security.authorization-engine.native", Version: "0.1.0", Configuration: configuration},
		Exchange: []authorizationv1.ProviderRef{},
	}
}

func RootDomain(catalog pluginv1.PermissionCatalog, profile authorizationv1.ProviderProfile) (authorizationv1.PolicyDomain, error) {
	if catalog.Digest == "" || profile.ID == "" {
		return authorizationv1.PolicyDomain{}, errors.New("构建根 Domain 需要权限目录和 Provider Profile")
	}
	permissions := make([]string, 0, len(catalog.Permissions))
	for _, entry := range catalog.Permissions {
		permissions = append(permissions, entry.Code)
	}
	sort.Strings(permissions)
	return authorizationv1.PolicyDomain{ID: "platform.root", Revision: 1, Kind: authorizationv1.DomainPlatform, ProviderProfileID: profile.ID, Delegation: authorizationv1.DelegationCeiling{Permissions: permissions, MaxRisk: authorizationv1.RiskCritical, MayDelegate: true, OfflineAllowed: false, MaxTTLSeconds: 300}}, nil
}
