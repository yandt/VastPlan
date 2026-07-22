package pluginv1

import "testing"

func TestAuthenticationProviderContributionRequiresRuntimeDependencies(t *testing.T) {
	raw := []byte(`{
      "id":"cn.example.identity.oidc","name":"oidc","description":"enterprise oidc provider",
      "version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
      "runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue",
        "requires":[{"capability":"network.egress.identity","scope":"remote","kind":"strong","ready":"readiness","failurePolicy":"retry"}]},
      "activation":["onStartup"],"entry":{"backend":"backend/main"},
      "contributes":{"backend":{"authenticationProviders":[{
        "id":"enterprise-oidc","service_role":"backend","protocol":"authentication.method.v1",
        "purposes":["portal-login","token-verification"],
        "methods":[{"id":"corporate-sso","kind":"redirect","interaction":"redirect"}],
        "subjectNamespace":"enterprise.identity.corporate",
        "requiredCapabilities":["network.egress.identity"]
      }]}}
    }`)
	manifest, err := ParseManifest(raw)
	if err != nil {
		t.Fatalf("合法 Authentication Provider 清单应通过: %v", err)
	}
	contributions, err := BackendRuntimeContributions(manifest)
	if err != nil || len(contributions) != 1 || contributions[0].ExtensionPoint != "authentication.provider" {
		t.Fatalf("Provider 贡献未规范化: %+v err=%v", contributions, err)
	}

	missing := []byte(`{
      "id":"cn.example.identity.database","name":"database","description":"database identity provider",
      "version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
      "runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue"},
      "activation":["onStartup"],"entry":{"backend":"backend/main"},
      "contributes":{"backend":{"authenticationProviders":[{
        "id":"database-users","service_role":"backend","protocol":"authentication.method.v1",
        "purposes":["portal-login"],"methods":[{"id":"password","kind":"password","interaction":"form"}],
        "subjectNamespace":"enterprise.identity.database","requiredCapabilities":["database.provider"]
      }]}}
    }`)
	if _, err := ParseManifest(missing); err == nil {
		t.Fatal("Provider 依赖不得只写在贡献中而绕过 runtime.requires")
	}
}

func TestAuthenticationProviderDescriptorRejectsAuthorizationData(t *testing.T) {
	invalid := []byte(`{
      "protocol":"authentication.method.v1","purposes":["portal-login"],
      "methods":[{"id":"password","kind":"password","interaction":"form"}],
      "subjectNamespace":"enterprise.identity.database","requiredCapabilities":[],
      "roles":["platform.admin"]
    }`)
	if err := ValidateDescriptor("authentication.provider", invalid); err == nil {
		t.Fatal("Authentication Provider descriptor 不得携带授权角色")
	}
}
