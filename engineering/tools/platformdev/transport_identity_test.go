package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
)

func TestWriteDevelopmentTransportIdentitiesCreatesMutuallyTrustedWorkloads(t *testing.T) {
	dir := t.TempDir()
	if err := writeDevelopmentTransportIdentities(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{platformNodeTransportSeed, managedNodeTransportSeed, portalHostTransportSeed} {
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
	if err := json.Unmarshal(raw, &document); err != nil || len(document.Identities) != 3 {
		t.Fatalf("传输信任文档无效: document=%+v err=%v", document, err)
	}
	if !document.Identities[2].AllowDelegation || document.Identities[0].NodeID != "local-platform-node" || document.Identities[1].NodeID != "local-managed-node" {
		t.Fatalf("Portal 委托或节点身份未锁定: %+v", document.Identities)
	}
}
