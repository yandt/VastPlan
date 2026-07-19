package nodeagent

import "testing"

func TestProtocolRuntime_LegacyInstalledPluginDefaultsToNative(t *testing.T) {
	runtime := &ProtocolRuntime{}
	driver, plugin, err := runtime.resolveExecutionDriver(InstalledPlugin{ID: "legacy", Publisher: "vastplan", EntryPath: "/tmp/legacy"})
	if err != nil {
		t.Fatal(err)
	}
	if driver.Name() != "native" || plugin.Execution.Driver != "native" {
		t.Fatalf("旧实际态应按 native 驱动启动: driver=%s plugin=%+v", driver.Name(), plugin)
	}
}
