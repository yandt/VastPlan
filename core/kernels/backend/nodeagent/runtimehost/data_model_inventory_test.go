package runtimehost

import (
	"strings"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
)

func TestExtractTrustedDataModelInventoryIsHostOnlyAndRemovedFromPluginConfig(t *testing.T) {
	digest := strings.Repeat("a", 64)
	plugins := []InstalledPlugin{{
		ID: "record-runtime",
		Contract: PluginRuntimeContract{Contributions: []pluginv1.RuntimeContribution{{
			ExtensionPoint: "tool.package", ID: recordstorev1.Capability, ContractVersion: recordstorev1.ContractVersion,
		}}},
	}}
	values := map[string]map[string]any{
		"record-runtime": {
			"ordinary": true,
			recordstorev1.TrustedInventoryConfigKey: map[string]any{
				"generation": uint64(3), "inventoryDigest": digest, "models": []any{},
			},
		},
	}
	inventory, err := extractTrustedDataModelInventory(plugins, values)
	if err != nil {
		t.Fatal(err)
	}
	if inventory == nil || inventory.Generation != 3 || inventory.InventoryDigest != digest {
		t.Fatalf("可信 Inventory 解码错误: %+v", inventory)
	}
	if _, leaked := values["record-runtime"][recordstorev1.TrustedInventoryConfigKey]; leaked {
		t.Fatal("宿主 Inventory 不得继续暴露给插件启动配置或 kernel.config")
	}
	values["ordinary"] = map[string]any{recordstorev1.TrustedInventoryConfigKey: map[string]any{}}
	if _, err := extractTrustedDataModelInventory(plugins, values); err == nil {
		t.Fatal("非 Record Store 插件不得接收宿主 Inventory")
	}
}
