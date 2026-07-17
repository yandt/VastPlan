package backendplugins

import (
	"context"
	"os"
	"testing"

	bootstrapembedded "cdsoft.com.cn/VastPlan/plugins/com.vastplan.foundation.security.bootstrap-policy/embedded"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/shared/go/registry"
)

func TestBootstrapPolicyStaticDefinitionMatchesManifest(t *testing.T) {
	raw, err := os.ReadFile("../../plugins/com.vastplan.foundation.security.bootstrap-policy/vastplan.plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	reg.DefinePoint(registry.ExtensionPoint{Name: extpoint.PermissionChecker, Dispatch: registry.DispatchSelect})
	host := protocolbus.NewHost("backend", "1.0.0", reg, nil)
	process, err := host.LaunchEmbeddedWithPolicy(context.Background(), bootstrapembedded.Definition(), protocolbus.LaunchPolicy{
		PluginID: manifest.ID, Version: manifest.Version, Contributions: contributions,
	})
	if err != nil {
		t.Fatalf("静态定义必须与签名清单严格一致: %v", err)
	}
	if process.RuntimeKind() != "embedded" || !process.Alive() {
		t.Fatalf("自举策略未以内嵌实例激活: %+v", process)
	}
}
