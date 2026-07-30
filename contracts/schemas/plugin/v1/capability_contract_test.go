package pluginv1

import (
	"fmt"
	"testing"
)

func TestManifestToolCapabilityContractsJoinsSignedAuthorization(t *testing.T) {
	manifest, err := ParseManifest([]byte(`{
    "id":"cn.vastplan.platform.demo","name":"Demo","description":"Demo","version":"1.0.0","publisher":"vastplan",
    "engines":{"backend":"^0.1"},
    "authorization":{"namespace":"platform.demo","permissions":[{"code":"platform.demo.read","title":"Read","scope":"platform","risk":"low","assignable":true,"offlineAllowed":false}],"operationGuards":[{"extensionPoint":"tool.package","capability":"platform.demo","operation":"list","permissions":["platform.demo.read"],"access":"read","approval":"none"}]},
    "activation":["onStartup"],"entry":{"backend":"backend/main"},
    "contributes":{"backend":{"tools":[{"id":"platform.demo","service_role":"backend","subcommands":[{"name":"list","description":"List","audience":"user"}]}]}}
  }`))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := ManifestToolCapabilityContracts(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(contracts) != 1 || len(contracts[0].Operations) != 1 || contracts[0].Operations[0].Audience != "user" || contracts[0].Operations[0].Guard == nil {
		t.Fatalf("unexpected contracts: %+v", contracts)
	}
}

func TestManifestToolCapabilityContractsRequiresClosedMigratedTool(t *testing.T) {
	base := `{
    "id":"cn.vastplan.platform.demo","name":"Demo","description":"Demo","version":"1.0.0","publisher":"vastplan",
    "engines":{"backend":"^0.1"},
    "authorization":{"namespace":"platform.demo","permissions":[{"code":"platform.demo.read","title":"Read","scope":"platform","risk":"low","assignable":true,"offlineAllowed":false}],"operationGuards":[{"extensionPoint":"tool.package","capability":"platform.demo","operation":"list","permissions":["platform.demo.read"],"access":"read","approval":"none"}]},
    "activation":["onStartup"],"entry":{"backend":"backend/main"},
    "contributes":{"backend":{"tools":[{"id":"platform.demo","service_role":"backend","subcommands":[%s]}]}}
  }`
	tests := []string{
		`{"name":"list","description":"List","audience":"user"},{"name":"write","description":"Write"}`,
		`{"name":"list","description":"List","audience":"workload"}`,
	}
	for _, operations := range tests {
		if _, err := ParseManifest([]byte(fmt.Sprintf(base, operations))); err == nil {
			t.Fatalf("expected closed capability contract rejection: %s", operations)
		}
	}
}
