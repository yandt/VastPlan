package backendcompositionv1

import (
	"encoding/json"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func TestParsePlatformProfileAndApplicationComposition(t *testing.T) {
	profile, err := ParsePlatformProfile([]byte(`{
		"version":1,"revision":2,"id":"backend-default","target":{"kernel":"backend"},
		"serviceClasses":["application.backend"],
		"serviceBaselines":[{"id":"application-security","serviceClass":"application.backend","plugins":[{"id":"cn.vastplan.foundation.security.portal-access-policy","version":"1.0.0"}],"config":{"security":{"mode":"enforced"}}}],
		"services":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if profile.ServiceBaselines[0].Plugins[0].Channel != "stable" || len(profile.Digest()) != 64 {
		t.Fatalf("Backend Platform Profile 未规范化: %+v", profile)
	}

	application, err := ParseApplicationComposition([]byte(`{
		"version":1,"revision":4,"id":"agent-studio","target":{"kernel":"backend"},"metadata":{"name":"agent-studio","tenant":"acme"},
		"units":[{"serviceClass":"application.backend","spec":{"id":"api","kind":"service","plugins":[{"id":"com.example.agent","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":1}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if application.Units[0].Spec.Plugins[0].Channel != "stable" || len(application.Digest()) != 64 {
		t.Fatalf("Backend Application Composition 未规范化: %+v", application)
	}
}

func TestBackendPlatformCatalogAuthorizesExactDeployment(t *testing.T) {
	profile, err := ParsePlatformProfile([]byte(`{
		"version":1,"revision":2,"id":"backend-default","target":{"kernel":"backend"},
		"serviceClasses":["application.backend"],"serviceBaselines":[],"services":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	catalog := BackendPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 3, ID: "production-backend"},
		Profiles: []PlatformProfile{profile},
		Bindings: []BackendPlatformBinding{{
			TenantID: "acme", DeploymentName: "agent-services",
			PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()},
		}},
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseBackendPlatformCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	resolved, _, err := parsed.Resolve("acme", "agent-services")
	if err != nil || resolved.ID != profile.ID {
		t.Fatalf("精确绑定未解析: profile=%+v err=%v", resolved, err)
	}
	if _, _, err := parsed.Resolve("acme", "unapproved"); err == nil {
		t.Fatal("未授权部署名必须 fail-closed")
	}
	catalog.Bindings = append(catalog.Bindings, catalog.Bindings[0])
	if _, err := ValidateBackendPlatformCatalog(catalog); err == nil {
		t.Fatal("重复 tenant/deployment 绑定必须拒绝")
	}
}

func TestCompositionSchemasRejectCrossBoundaryFields(t *testing.T) {
	invalidProfiles := []string{
		`{"version":1,"revision":1,"id":"default","target":{"kernel":"frontend"},"serviceClasses":["application.backend"],"serviceBaselines":[],"services":[]}`,
		`{"version":1,"revision":1,"id":"default","target":{"kernel":"backend"},"serviceClasses":["application.backend"],"serviceBaselines":[{"id":"unknown","serviceClass":"unknown","plugins":[{"id":"com.example.x","version":"1.0.0"}]}],"services":[]}`,
	}
	for _, raw := range invalidProfiles {
		if _, err := ParsePlatformProfile([]byte(raw)); err == nil {
			t.Fatal("非法 Backend Platform Profile 必须拒绝")
		}
	}
	invalidApplication := `{"version":1,"revision":1,"id":"app","target":{"kernel":"backend"},"metadata":{"name":"app"},"platformPlugins":[],"units":[]}`
	if _, err := ParseApplicationComposition([]byte(invalidApplication)); err == nil {
		t.Fatal("Backend Application Composition 不得携带平台配置字段")
	}
	legacyApplication := `{"version":1,"revision":1,"id":"app","kernel":"backend","metadata":{"name":"app"},"units":[]}`
	if _, err := ParseApplicationComposition([]byte(legacyApplication)); err == nil {
		t.Fatal("旧 kernel 字段不得绕过统一 target 契约")
	}
}
