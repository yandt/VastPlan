package compositionv1

import "testing"

func TestParsePlatformProfileAndApplicationComposition(t *testing.T) {
	profile, err := ParsePlatformProfile([]byte(`{
		"version":1,"revision":2,"id":"backend-default","target":{"kernel":"backend"},
		"serviceClasses":["application.backend"],
		"attachments":[{"serviceClass":"application.backend","plugins":[{"id":"com.vastplan.foundation.security.portal-access-policy","version":"1.0.0"}]}],
		"services":[]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if profile.Attachments[0].Plugins[0].Channel != "stable" || len(profile.Digest()) != 64 {
		t.Fatalf("Platform Profile 未规范化: %+v", profile)
	}

	application, err := ParseApplicationComposition([]byte(`{
		"version":1,"revision":4,"id":"agent-studio","kernel":"backend","metadata":{"name":"agent-studio","tenant":"acme"},
		"units":[{"serviceClass":"application.backend","spec":{"id":"api","kind":"service","plugins":[{"id":"com.example.agent","version":"1.0.0"}],"enabled":true,"service_role":"backend","replicas":1}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if application.Units[0].Spec.Plugins[0].Channel != "stable" || len(application.Digest()) != 64 {
		t.Fatalf("Application Composition 未规范化: %+v", application)
	}
}

func TestCompositionSchemasRejectCrossBoundaryFields(t *testing.T) {
	invalidProfiles := []string{
		`{"version":1,"revision":1,"id":"default","target":{"kernel":"portal"},"serviceClasses":["application.backend"],"attachments":[],"services":[]}`,
		`{"version":1,"revision":1,"id":"default","target":{"kernel":"backend"},"serviceClasses":["application.backend"],"attachments":[{"serviceClass":"unknown","plugins":[{"id":"com.example.x","version":"1.0.0"}]}],"services":[]}`,
	}
	for _, raw := range invalidProfiles {
		if _, err := ParsePlatformProfile([]byte(raw)); err == nil {
			t.Fatal("非法 Platform Profile 必须拒绝")
		}
	}
	invalidApplication := `{"version":1,"revision":1,"id":"app","kernel":"backend","metadata":{"name":"app"},"platformPlugins":[],"units":[]}`
	if _, err := ParseApplicationComposition([]byte(invalidApplication)); err == nil {
		t.Fatal("Application Composition 不得携带平台配置字段")
	}
}
