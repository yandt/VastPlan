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
		"engines":{"backend":"^1.0"},"capabilities":{"kernelServices":["kernel.config.credential-ref"]},
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

func TestControllerPolicyAllowsLeaderWithSharedState(t *testing.T) {
	contribution := pluginv1.RuntimeContribution{
		InstancePolicy: "leader",
		StateModel:     "external-shared",
		Visibility:     "cluster",
		Routing:        "leader",
	}
	if err := validateControllerRuntimePolicy(contribution, pluginv1.ConfigurationControllerProtocol); err != nil {
		t.Fatalf("单 leader 配置控制器应允许使用可接管的共享账本: %v", err)
	}
}

func TestBuildDerivesHotControllerFromSignedArtifactAndResolvedUnit(t *testing.T) {
	const pluginID = "cn.vastplan.demo-hot-configured"
	manifest := []byte(fmt.Sprintf(`{
		"id":%q,"name":"Hot plugin","description":"hot configured","version":"1.0.0","publisher":"vastplan",
		"engines":{"backend":"^0.1"},
		"runtime":{"instancePolicy":"leader","stateModel":"leader-owned","visibility":"cluster","routing":"leader","routingDomain":"security"},
		"configuration":{"scope":"service","applyMode":"hot","controller":{"protocol":"configuration.v1"},"schema":{"type":"object","additionalProperties":false,"properties":{"capacity":{"type":"integer"}}}},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}
	}`, pluginID))
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.0.0", Channel: "stable"}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 9, Metadata: deploymentv1.Metadata{Name: "security-services", Tenant: "acme"},
		Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{pluginID: deploymentv2.OriginPlatformProfile}},
		Units: []deploymentv2.ServiceUnit{{
			ID: "otp", Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: "authentication-otp", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}},
			Config:  map[string]any{"plugins": map[string]any{pluginID: map[string]any{"capacity": 100}}},
		}},
	}
	catalog, err := Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {
		PluginID: pluginID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("b", 64), Manifest: manifest,
	}})
	if err != nil {
		t.Fatal(err)
	}
	item := catalog.Items[0]
	wantCapability, _ := pluginv1.ConfigurationControllerCapability(pluginID)
	if item.ApplyPath != ApplyHotService || item.Controller == nil || item.Controller.Capability != wantCapability ||
		item.Controller.ExtensionPoint != pluginv1.ConfigurationControllerExtensionPoint || item.Controller.LogicalService != "authentication-otp" || item.Controller.RoutingDomain != "security" {
		t.Fatalf("hot controller 未由可信制品与解析单元精确派生: %+v", item.Controller)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	tampered := catalog
	tampered.Items = append([]Definition(nil), catalog.Items...)
	controller := *tampered.Items[0].Controller
	controller.Capability = "configuration.forged"
	tampered.Items[0].Controller = &controller
	if err := tampered.Validate(); err == nil {
		t.Fatal("目录不得接受与插件身份不一致的 controller capability")
	}
}

func TestBuildDerivesIndependentResourceCollections(t *testing.T) {
	const pluginID = "cn.vastplan.demo-profile-provider"
	manifest := []byte(fmt.Sprintf(`{
		"id":%q,"name":"Profile provider","description":"profiles","version":"1.0.0","publisher":"vastplan",
		"engines":{"backend":"^0.1"},
		"runtime":{"instancePolicy":"leader","stateModel":"leader-owned","visibility":"cluster","routing":"leader","routingDomain":"security"},
		"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false},
			"resourceController":{"protocol":"configuration.resource.v1"},
			"resourceCollections":[{"id":"delivery-profile","kind":"profile","title":"Delivery Profile","schema":{"type":"object","additionalProperties":false,"required":["endpoint"],"properties":{"endpoint":{"type":"string"}}},"managedCredentials":[{"id":"authorization","title":"Authorization","purpose":"delivery.authorization","required":true}],"maxItems":64}]},
		"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}
	}`, pluginID))
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: "1.0.0", Channel: "stable"}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 10, Metadata: deploymentv1.Metadata{Name: "security-services", Tenant: "acme"},
		Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{pluginID: deploymentv2.OriginPlatformProfile}},
		Units: []deploymentv2.ServiceUnit{{
			ID: "delivery", Kind: "service", Enabled: true, ServiceRole: "backend", LogicalService: "authentication-delivery", Replicas: 1,
			Plugins: []deploymentv1.PluginRef{{ID: pluginID, Version: "1.0.0", Channel: "stable"}},
			Config:  map[string]any{"plugins": map[string]any{pluginID: map[string]any{}}},
		}},
	}
	catalog, err := Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{ref: {
		PluginID: pluginID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("d", 64), Manifest: manifest,
	}})
	if err != nil {
		t.Fatal(err)
	}
	item := catalog.Items[0]
	wantCapability, _ := pluginv1.ConfigurationResourceControllerCapability(pluginID)
	wantCollection, _ := pluginv1.ConfigurationResourceCollectionID(pluginID, "delivery-profile")
	if item.ResourceController == nil || item.ResourceController.Capability != wantCapability || item.ResourceController.LogicalService != "authentication-delivery" ||
		len(item.ResourceCollections) != 1 || item.ResourceCollections[0].ID != wantCollection || item.ResourceCollections[0].Kind != "profile" ||
		len(item.ResourceCollections[0].ManagedCredentials) != 1 {
		t.Fatalf("资源控制器或集合未由签名制品派生: %+v", item)
	}
	if err := catalog.Validate(); err != nil {
		t.Fatal(err)
	}
	tampered := catalog
	tampered.Items = append([]Definition(nil), catalog.Items...)
	collections := append([]ResourceCollection(nil), item.ResourceCollections...)
	collections[0].SchemaDigest = strings.Repeat("0", 64)
	tampered.Items[0].ResourceCollections = collections
	if err := tampered.Validate(); err == nil {
		t.Fatal("目录不得接受被替换的资源 Schema 摘要")
	}
}
