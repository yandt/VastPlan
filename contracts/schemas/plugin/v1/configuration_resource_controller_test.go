package pluginv1_test

import (
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestResourceControllerSynthesizesOpaqueContributionAndCollectionID(t *testing.T) {
	manifest, err := pluginv1.ParseManifest([]byte(resourceControllerManifest()))
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	wantCapability, _ := pluginv1.ConfigurationResourceControllerCapability(manifest.ID)
	wantCollection, _ := pluginv1.ConfigurationResourceCollectionID(manifest.ID, "delivery-profile")
	if wantCapability != "configuration.resource.04542ba916aaa20c436092fd98f2bdbd" || wantCollection != "cfgc_88a57d6eb44c31080cd5d2b9" {
		t.Fatalf("resource controller golden 漂移: capability=%s collection=%s", wantCapability, wantCollection)
	}
	found := false
	for _, contribution := range contributions {
		if contribution.ExtensionPoint == pluginv1.ConfigurationResourceControllerExtensionPoint {
			found = contribution.ID == wantCapability && string(contribution.Descriptor) == `{"protocol":"configuration.resource.v1"}`
		}
	}
	if !found {
		t.Fatalf("签名资源契约未合成 runtime contribution: %+v", contributions)
	}
}

func TestResourceCollectionsRequireControllerAndClosedSchema(t *testing.T) {
	invalid := strings.Replace(resourceControllerManifest(), `,"resourceController":{"protocol":"configuration.resource.v1"}`, "", 1)
	if _, err := pluginv1.ParseManifest([]byte(invalid)); err == nil {
		t.Fatal("资源集合缺少控制器不得通过")
	}
	invalid = strings.Replace(resourceControllerManifest(), `"additionalProperties":false,"required":["endpoint"]`, `"additionalProperties":true,"required":["endpoint"]`, 1)
	if _, err := pluginv1.ParseManifest([]byte(invalid)); err == nil {
		t.Fatal("资源集合必须使用闭合 Schema")
	}
}

func resourceControllerManifest() string {
	return `{"id":"cn.vastplan.demo-resource-controller","name":"Demo","description":"Demo resources","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},"runtime":{"instancePolicy":"leader","stateModel":"leader-owned","visibility":"cluster","routing":"leader","routingDomain":"demo"},"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false},"resourceController":{"protocol":"configuration.resource.v1"},"resourceCollections":[{"id":"delivery-profile","kind":"profile","title":"Delivery Profile","schema":{"type":"object","additionalProperties":false,"required":["endpoint"],"properties":{"endpoint":{"type":"string"}}},"managedCredentials":[{"id":"authorization","title":"Authorization","purpose":"demo.authorization","required":true}],"maxItems":64}]},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`
}
