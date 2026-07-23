package main

import (
	"os"
	"path/filepath"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
)

func TestManagedServiceCompositionPinsCurrentHelloWorldArtifact(t *testing.T) {
	compositionPath := filepath.Join("..", "..", "deploy", "managed-services-application.json")
	composition, err := backendcompositionv1.ParseApplicationCompositionFile(compositionPath)
	if err != nil {
		t.Fatal(err)
	}
	if composition.Metadata.Name != "managed-services" || composition.Metadata.Tenant != "local" || len(composition.Units) != 1 {
		t.Fatalf("本地在线服务组合身份无效: %+v", composition)
	}
	unit := composition.Units[0].Spec
	if unit.Placement.NodeSelector["environment"] != "local-managed" || len(unit.Plugins) != 1 || unit.Plugins[0].ID != "cn.vastplan.hello-world" {
		t.Fatalf("本地在线服务组合未精确绑定 hello-world: %+v", unit)
	}

	manifestPath := filepath.Join("..", "..", "..", "extensions", "plugins", "cn.vastplan.hello-world", "vastplan.plugin.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if unit.Plugins[0].Version != manifest.Version {
		t.Fatalf("平台开发组合仍引用 hello-world@%s，当前可发布制品为 %s", unit.Plugins[0].Version, manifest.Version)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil || len(contributions) != 1 {
		t.Fatalf("解析 hello-world runtime contribution: contributions=%+v err=%v", contributions, err)
	}
	contribution := contributions[0]
	if !servicemodel.Equal(
		servicemodel.Policy{InstancePolicy: unit.InstancePolicy, StateModel: unit.StateModel, Visibility: unit.Visibility, Routing: unit.Routing, RoutingDomain: unit.RoutingDomain},
		servicemodel.Policy{InstancePolicy: contribution.InstancePolicy, StateModel: contribution.StateModel, Visibility: contribution.Visibility, Routing: contribution.Routing, RoutingDomain: contribution.RoutingDomain},
	) {
		t.Fatalf("平台开发组合策略与 hello-world 签名清单不一致: unit=%+v contribution=%+v", unit, contribution)
	}
	plugins, ok := unit.Config["plugins"].(map[string]any)
	if !ok {
		t.Fatalf("hello-world Scoped Seed 容器缺失: %+v", unit.Config)
	}
	values, ok := plugins["cn.vastplan.hello-world"].(map[string]any)
	if !ok || values["greetingTemplate"] == "" {
		t.Fatalf("hello-world Scoped Seed 缺失: %+v", unit.Config)
	}
}
