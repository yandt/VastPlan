package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func TestStablePackageIdentityLedgerRejectsSameRefWithDifferentBytes(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "identity", "stable.json")
	first := writeStableIdentityTestRepository(t, "1.0.0", "first")
	if err := reconcileStablePackageIdentities(first, ledgerPath); err != nil {
		t.Fatal(err)
	}
	if err := reconcileStablePackageIdentities(first, ledgerPath); err != nil {
		t.Fatalf("相同制品必须幂等: %v", err)
	}
	conflict := writeStableIdentityTestRepository(t, "1.0.0", "changed")
	err := reconcileStablePackageIdentities(conflict, ledgerPath)
	if err == nil || !strings.Contains(err.Error(), "提升插件 SemVer") || !strings.Contains(err.Error(), "cn.vastplan.test.identity@1.0.0/stable") {
		t.Fatalf("同一 stable ref 的不同字节必须提前失败: %v", err)
	}
	upgraded := writeStableIdentityTestRepository(t, "1.0.1", "changed")
	if err := reconcileStablePackageIdentities(upgraded, ledgerPath); err != nil {
		t.Fatalf("提升 SemVer 后应记录新身份: %v", err)
	}
	raw, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	var ledger stablePackageIdentityLedger
	if err := json.Unmarshal(raw, &ledger); err != nil {
		t.Fatal(err)
	}
	if len(ledger.Artifacts) != 2 {
		t.Fatalf("账本必须保留历史稳定版本: %#v", ledger.Artifacts)
	}
	info, err := os.Stat(ledgerPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("身份账本必须仅属主可写: info=%v err=%v", info, err)
	}
}

func TestStablePackageIdentityLedgerFailsClosedWhenCorrupted(t *testing.T) {
	root := t.TempDir()
	ledgerPath := filepath.Join(root, "stable.json")
	if err := os.WriteFile(ledgerPath, []byte(`{"schema":1,"artifacts":[{"pluginId":"cn.vastplan.test","version":"1.0.0","channel":"stable","sha256":"bad"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := reconcileStablePackageIdentities(writeStableIdentityTestRepository(t, "1.0.0", "first"), ledgerPath)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 无效") {
		t.Fatalf("损坏账本不得被静默覆盖: %v", err)
	}
}

func TestStablePackageIdentityLedgerScopesDynamicGoByBuildFingerprint(t *testing.T) {
	ledgerPath := filepath.Join(t.TempDir(), "stable.json")
	firstFingerprint := strings.Repeat("a", 64)
	secondFingerprint := strings.Repeat("b", 64)
	if err := reconcileStablePackageIdentities(writeStableIdentityDynamicRepository(t, firstFingerprint, "first"), ledgerPath); err != nil {
		t.Fatal(err)
	}
	if err := reconcileStablePackageIdentities(writeStableIdentityDynamicRepository(t, secondFingerprint, "second"), ledgerPath); err != nil {
		t.Fatalf("不同 Backend 共同构建指纹是不同 dynamic-go variant: %v", err)
	}
	err := reconcileStablePackageIdentities(writeStableIdentityDynamicRepository(t, secondFingerprint, "changed"), ledgerPath)
	if err == nil || !strings.Contains(err.Error(), "variant="+secondFingerprint) {
		t.Fatalf("同一 dynamic-go variant 的不同字节仍必须拒绝: %v", err)
	}
}

func writeStableIdentityTestRepository(t *testing.T, version, content string) string {
	return writeStableIdentityRepository(t, version, content, "")
}

func writeStableIdentityDynamicRepository(t *testing.T, fingerprint, content string) string {
	return writeStableIdentityRepository(t, "1.0.0", content, fingerprint)
}

func writeStableIdentityRepository(t *testing.T, version, content, fingerprint string) string {
	t.Helper()
	pluginDir := t.TempDir()
	execution := ""
	if fingerprint != "" {
		execution = `,"execution":{"backend":{"driver":"native","minimumIsolation":"trusted-process","dynamicGo":{"entry":"backend/plugin.so","abi":"vastplan.dynamic-go.v1","fingerprint":"` + fingerprint + `","required":true}}}`
	}
	manifest := `{
  "id":"cn.vastplan.test.identity",
  "name":"Stable identity test",
  "description":"stable identity fixture",
  "version":"` + version + `",
  "publisher":"vastplan",
  "engines":{"backend":"^1.0"}` + execution + `,
  "activation":["onStartup"],
  "entry":{"backend":"backend/main"},
  "contributes":{"backend":{"tools":[{"id":"test.identity","service_role":"backend","title":"Identity fixture","subcommands":[{"name":"run","description":"run"}]}]}}
}`
	if err := os.WriteFile(filepath.Join(pluginDir, "vastplan.plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(pluginDir, "backend"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "backend", "main"), []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	if fingerprint != "" {
		if err := os.WriteFile(filepath.Join(pluginDir, "backend", "plugin.so"), []byte("so-"+content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	packageBytes, _, err := pluginservice.PackageDirectory(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot := t.TempDir()
	repository, err := pluginservice.NewRepository(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Publish("stable", packageBytes); err != nil {
		t.Fatal(err)
	}
	return repositoryRoot
}
