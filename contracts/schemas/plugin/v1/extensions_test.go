package pluginv1

import (
	"strings"
	"testing"
)

const extensionOwnerManifest = `{
  "id":"cn.vastplan.foundation.account","name":"Account","description":"Account extension host",
  "version":"1.2.0","publisher":"vastplan","engines":{"frontend":"^1.0"},
  "activation":["onPortalStartup"],"entry":{"frontend":"frontend/index.js"},
  "extensionPoints":[{"id":"cn.vastplan.foundation.account.page","surface":"frontend","contract":"1.0.0","kind":"frontend.page","dispatch":"mount","targets":["account"],"descriptorSchema":{"type":"object","additionalProperties":false,"required":["pageId","groupId"],"properties":{"pageId":{"type":"string"},"groupId":{"const":"account"}}}}],
  "contributes":{"frontend":{"views":[{"id":"account","title":"Account"}]}}
}`

func TestResolveExtensionGraphValidatesOwnerDependencyContractAndDescriptor(t *testing.T) {
	owner, err := ParseManifest([]byte(extensionOwnerManifest))
	if err != nil {
		t.Fatal(err)
	}
	contributor, err := ParseManifest([]byte(`{
    "id":"cn.vastplan.example.account-security","name":"Security","description":"Account security page",
    "version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},
    "activation":["onPortalStartup"],"entry":{"frontend":"frontend/index.js"},
    "dependencies":{"cn.vastplan.foundation.account":"^1.2.0"},
    "extensions":[{"point":"cn.vastplan.foundation.account.page","surface":"frontend","contract":"^1.0.0","id":"cn.vastplan.example.account-security.page","order":20,"descriptor":{"pageId":"account.security","groupId":"account"}}],
    "contributes":{"frontend":{"views":[{"id":"account.security","title":"Security"}]}}
  }`))
	if err != nil {
		t.Fatal(err)
	}
	graph, err := ResolveExtensionGraph([]Manifest{contributor, owner}, "frontend")
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Points) != 1 || len(graph.Contributions) != 1 || graph.Points[0].OwnerPluginID != owner.ID || graph.Contributions[0].PluginID != contributor.ID {
		t.Fatalf("扩展图解析错误: %+v", graph)
	}
}

func TestResolveExtensionGraphRejectsMissingOwnerDependencyAndInvalidDescriptor(t *testing.T) {
	owner, err := ParseManifest([]byte(extensionOwnerManifest))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, dependency, descriptor string }{
		{name: "missing dependency", dependency: "", descriptor: `{"pageId":"account.security","groupId":"account"}`},
		{name: "invalid descriptor", dependency: `"dependencies":{"cn.vastplan.foundation.account":"^1.2.0"},`, descriptor: `{"pageId":"account.security","groupId":"settings"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := `{"id":"cn.vastplan.example.account-security","name":"Security","description":"Account security page","version":"1.0.0","publisher":"vastplan","engines":{"frontend":"^1.0"},"activation":["onPortalStartup"],"entry":{"frontend":"frontend/index.js"},` + test.dependency + `"extensions":[{"point":"cn.vastplan.foundation.account.page","surface":"frontend","contract":"^1.0.0","id":"cn.vastplan.example.account-security.page","descriptor":` + test.descriptor + `}],"contributes":{"frontend":{"views":[{"id":"account.security","title":"Security"}]}}}`
			contributor, parseErr := ParseManifest([]byte(raw))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if _, resolveErr := ResolveExtensionGraph([]Manifest{owner, contributor}, "frontend"); resolveErr == nil {
				t.Fatal("无效扩展关系必须被拒绝")
			}
		})
	}
}

func TestParseManifestRejectsExtensionPointOutsideOwnerNamespace(t *testing.T) {
	raw := strings.Replace(extensionOwnerManifest, "cn.vastplan.foundation.account.page", "cn.vastplan.other.page", 1)
	if _, err := ParseManifest([]byte(raw)); err == nil {
		t.Fatal("插件不得声明其他命名空间的扩展点")
	}
}
