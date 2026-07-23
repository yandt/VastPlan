package pluginv1_test

import (
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestHotServiceControllerSynthesizesOpaqueRuntimeContribution(t *testing.T) {
	manifest, err := pluginv1.ParseManifest([]byte(hotControllerManifest("hot", "service", true)))
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	want, err := pluginv1.ConfigurationControllerCapability(manifest.ID)
	if err != nil || strings.Contains(want, manifest.ID) {
		t.Fatalf("控制能力必须稳定且不暴露插件 ID: %q err=%v", want, err)
	}
	found := false
	for _, contribution := range contributions {
		if contribution.ExtensionPoint == pluginv1.ConfigurationControllerExtensionPoint {
			found = contribution.ID == want && string(contribution.Descriptor) == `{"protocol":"configuration.v1"}`
		}
	}
	if !found {
		t.Fatalf("签名配置契约未合成 controller runtime contribution: %+v", contributions)
	}
}

func TestConfigurationControllerIsOnlyValidForServiceHot(t *testing.T) {
	if _, err := pluginv1.ParseManifest([]byte(hotControllerManifest("restart", "service", true))); err == nil {
		t.Fatal("restart 配置不得声明 hot controller")
	}
	if _, err := pluginv1.ParseManifest([]byte(hotControllerManifest("hot", "tenant", true))); err == nil {
		t.Fatal("tenant hot 配置应使用 scoped resolver，不得声明 service controller")
	}
	manifest, err := pluginv1.ParseManifest([]byte(hotControllerManifest("hot", "service", false)))
	if err != nil {
		t.Fatalf("尚未实现 controller 的旧 hot 插件应保持只读可装载: %v", err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(contributions) != 0 {
		t.Fatalf("未声明 controller 不得虚构运行能力: %+v err=%v", contributions, err)
	}
}

func TestConfigurationControllerCapabilityMatchesNodeSDKGolden(t *testing.T) {
	capability, err := pluginv1.ConfigurationControllerCapability("cn.vastplan.example.hot-controller")
	if err != nil {
		t.Fatal(err)
	}
	if capability != "configuration.3c183e12decc8e57e3ea513837dc8708" {
		t.Fatalf("Go/Node controller capability 不一致: %s", capability)
	}
}

func hotControllerManifest(mode, scope string, controller bool) string {
	controllerJSON := ""
	if controller {
		controllerJSON = `,"controller":{"protocol":"configuration.v1"}`
	}
	return `{"id":"cn.vastplan.demo-hot-controller","name":"Demo","description":"Demo controller","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},"runtime":{"instancePolicy":"leader","stateModel":"leader-owned","visibility":"cluster","routing":"leader","routingDomain":"demo"},"configuration":{"scope":"` + scope + `","applyMode":"` + mode + `","schema":{"type":"object","additionalProperties":false}` + controllerJSON + `},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`
}
