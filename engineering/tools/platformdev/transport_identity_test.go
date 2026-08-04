package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
)

func TestWriteDevelopmentTransportIdentitiesCreatesMutuallyTrustedWorkloads(t *testing.T) {
	dir := t.TempDir()
	if err := writeDevelopmentTransportIdentities(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{platformNodeTransportSeed, managedNodeTransportSeed, portalHostTransportSeed, platformDevTransportSeed} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("传输 seed 必须仅属主可读: name=%s info=%v err=%v", name, info, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, transportTrustDocument))
	if err != nil {
		t.Fatal(err)
	}
	var document addressing.TransportTrustDocument
	if err := json.Unmarshal(raw, &document); err != nil || len(document.Identities) != 4 {
		t.Fatalf("传输信任文档无效: document=%+v err=%v", document, err)
	}
	if !document.Identities[2].AllowDelegation || document.Identities[0].NodeID != "local-platform-node" || document.Identities[1].NodeID != "local-managed-node" {
		t.Fatalf("Portal 委托或节点身份未锁定: %+v", document.Identities)
	}
	if document.Identities[0].Name != "node-agent/local-platform-node" || document.Identities[1].Name != "node-agent/local-managed-node" {
		t.Fatalf("Node transport identity 必须投影为策略可验证的 node-agent system caller: %+v", document.Identities)
	}
	if len(document.Identities[0].AllowedSystemCallers) != 2 || document.Identities[0].AllowedSystemCallers[1] != platformcontrolv1.TrustedBootstrapSystemID {
		t.Fatalf("平台节点未精确授权 Platform Control SYSTEM caller: %+v", document.Identities[0])
	}
	if document.Identities[3].Name != "platform-dev" || document.Identities[3].NodeID != "platform-dev" || len(document.Identities[3].AllowedSystemCallers) != 1 || document.Identities[3].AllowedSystemCallers[0] != developmentInstallationCaller {
		t.Fatalf("开发安装控制器身份未被精确绑定: %+v", document.Identities[3])
	}
}
