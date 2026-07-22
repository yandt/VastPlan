package pluginconfiguration

import (
	"fmt"
	"strings"
	"testing"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestBuildUsesVerifiedManifestAndOpaqueResourceIdentity(t *testing.T) {
	const pluginID = "com.example.configured"
	manifest := []byte(fmt.Sprintf(`{
		"id":%q,"name":"Configured plugin","description":"configured","version":"1.0.0","publisher":"example",
		"engines":{"backend":"^1.0"},
		"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false,"properties":{"region":{"type":"string"}}},"managedCredentials":[{"id":"token","title":"Token","purpose":"remote.token"}]},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}
	}`, pluginID))
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.0.0", Channel: "stable"}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 7, Metadata: deploymentv1.Metadata{Name: "agent-services", Tenant: "acme"},
		Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{pluginID: deploymentv2.OriginApplication}},
		Units: []deploymentv2.ServiceUnit{{ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}},
			Config:  map[string]any{"plugins": map[string]any{pluginID: map[string]any{"region": "cn-east"}}},
		}},
	}
	catalog, err := Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {PluginID: pluginID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("a", 64), Manifest: manifest}})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Items) != 1 {
		t.Fatalf("配置目录项数量错误: %+v", catalog)
	}
	item := catalog.Items[0]
	if item.ID == "" || strings.Contains(item.ID, pluginID) || item.ApplyPath != ApplyApplicationDeployment || string(item.Values) != `{"region":"cn-east"}` || len(item.ManagedCredentials) != 1 {
		t.Fatalf("配置目录未冻结可信配置事实: %+v", item)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatalf("生成目录必须可自校验: %v", err)
	}

	tampered := catalog
	tampered.Items = append([]Definition(nil), catalog.Items...)
	tampered.Items[0].Schema = []byte(`{"type":"object","additionalProperties":false}`)
	if err := tampered.Validate(); err == nil {
		t.Fatal("篡改 Schema 后目录必须拒绝")
	}
}

func TestBuildSeparatesPlatformAndHotApplyPaths(t *testing.T) {
	for _, test := range []struct {
		name, origin, scope, mode string
		want                      ApplyPath
	}{
		{name: "platform restart", origin: deploymentv2.OriginPlatformProfile, scope: "service", mode: "restart", want: ApplyPlatformProfile},
		{name: "service hot", origin: deploymentv2.OriginPlatformProfile, scope: "service", mode: "hot", want: ApplyHotService},
		{name: "user hot", origin: deploymentv2.OriginApplication, scope: "user", mode: "hot", want: ApplyHotScoped},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := applyPathFor(test.origin, test.scope, test.mode)
			if err != nil || got != test.want {
				t.Fatalf("生效路径错误: got=%q err=%v", got, err)
			}
		})
	}
	if _, err := applyPathFor(deploymentv2.OriginApplication, "tenant", "restart"); err == nil {
		t.Fatal("tenant restart 必须拒绝")
	}
}
