package pluginv1

import (
	"strings"
	"testing"
)

func TestParseManifestGovernedAPIContributions(t *testing.T) {
	manifest, err := ParseManifest(governedAPIManifest())
	if err != nil {
		t.Fatal(err)
	}
	contributions, err := ManifestAPIContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(contributions.Contracts) != 1 || len(contributions.DataPlaneServices) != 1 {
		t.Fatalf("声明式 API 贡献未完整解析: %+v", contributions)
	}
	runtime, err := BackendRuntimeContributions(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime) != 1 || runtime[0].ExtensionPoint != "tool.package" {
		t.Fatalf("声明式 API 不得直接注册协议总线: %+v", runtime)
	}
}

func TestAPIContractMayOnlyExposeOwnedToolOperation(t *testing.T) {
	for name, raw := range map[string]string{
		"未声明 capability": strings.Replace(string(governedAPIManifest()), `"capability":"platform.demo"`, `"capability":"platform.other"`, 1),
		"未声明 operation":  strings.Replace(string(governedAPIManifest()), `"operation":"listItems"`, `"operation":"deleteAll"`, 1),
		"跨 service role": strings.Replace(string(governedAPIManifest()), `"id":"management-api","service_role":"backend"`, `"id":"management-api","service_role":"workspace"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(raw)); err == nil {
				t.Fatal("越权 API 契约必须被拒绝")
			}
		})
	}
}

func governedAPIManifest() []byte {
	return []byte(`{
  "id":"com.example.governed-api","name":"governed-api","description":"governed api",
  "version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},
  "activation":["onStartup"],"entry":{"backend":"backend/main"},
  "contributes":{"backend":{
    "tools":[{"id":"platform.demo","service_role":"backend","subcommands":[{"name":"listItems","description":"list"}]}],
    "apiContracts":[{
      "id":"management-api","service_role":"backend","contractId":"platform.demo.api","contractVersion":"1.0.0","protocol":"http-json",
      "routes":[{"id":"platform.demo.list","method":"POST","path":"/items","target":{"capability":"platform.demo","operation":"listItems"},"requestSchema":{"type":"object"},"responseSchema":{"type":"object"},"successStatus":200}]
    }],
    "dataPlaneServices":[{"id":"object-store","service_role":"backend","protocol":"https","purposes":["artifact-download"],"supportedModes":["ticket-redirect"],"healthPath":"/healthz","maxObjectBytes":1073741824}]
  }}
}`)
}
