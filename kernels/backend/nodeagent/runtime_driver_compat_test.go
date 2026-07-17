package nodeagent

import "testing"

func TestProtocolRuntime_LegacyInstalledPluginDefaultsToNative(t *testing.T) {
	runtime := &ProtocolRuntime{}
	spec, err := runtime.launchSpec(InstalledPlugin{ID: "legacy", Publisher: "vastplan", EntryPath: "/tmp/legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != "/tmp/legacy" {
		t.Fatalf("旧实际态应按 native 驱动启动: %+v", spec)
	}
}
