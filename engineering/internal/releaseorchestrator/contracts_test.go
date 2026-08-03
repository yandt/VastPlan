package releaseorchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncContractsGeneratesOutputsAndDoesNotRewritePluginClaims(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ContractRegistryPath, "schemaVersion: 1\ncontracts:\n  frontend.ui:\n    version: 6.1.0\n    compatibility: ^6.0.0\n    description: test\n")
	writeFile(t, root, "engineering/deploy/portal-platform-catalog.json", `{"uiContract":"^5.0.0"}`)
	writeFile(t, root, "extensions/plugins/cn.vastplan.foundation.frontend.test/vastplan.plugin.json", `{"contributes":{"frontend":{"views":[{"uiContract":"^6.0.0"}]}}}`)
	writeFile(t, root, "extensions/plugins/cn.vastplan.foundation.frontend.test/frontend/src/index.ts", `export const value = { uiContract: uiContractVersion };`)
	writeFile(t, root, "examples/plugins/.keep/vastplan.plugin.json", `{}`)
	if _, err := SyncContracts(root, true); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(filepath.Join(root, "contracts/generated/go/contractregistry/registry.go"))
	if err != nil || !strings.Contains(string(generated), `FrontendUIContractMajor   = 6`) {
		t.Fatalf("Go registry 未生成: %s err=%v", generated, err)
	}
	catalog, err := os.ReadFile(filepath.Join(root, "engineering/deploy/portal-platform-catalog.json"))
	if err != nil || !strings.Contains(string(catalog), `"^6.0.0"`) {
		t.Fatalf("Catalog 未同步: %s err=%v", catalog, err)
	}
	manifest, _ := os.ReadFile(filepath.Join(root, "extensions/plugins/cn.vastplan.foundation.frontend.test/vastplan.plugin.json"))
	if !strings.Contains(string(manifest), `"^6.0.0"`) {
		t.Fatal("插件兼容声明不得由生成器重写")
	}
	if changes, err := SyncContracts(root, false); err != nil || len(changes) != 0 {
		t.Fatalf("同步后 check 必须通过: changes=%+v err=%v", changes, err)
	}
}

func TestSyncContractsKeepsCompatiblePortalProfileRangeOnMinorRelease(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ContractRegistryPath, "schemaVersion: 1\ncontracts:\n  frontend.ui:\n    version: 6.1.0\n    compatibility: ^6.1.0\n    description: test\n")
	writeFile(t, root, "engineering/deploy/portal-platform-catalog.json", `{"uiContract":"^6.0.0"}`)
	writeFile(t, root, "extensions/plugins/cn.vastplan.foundation.frontend.test/vastplan.plugin.json", `{"contributes":{"frontend":{"views":[{"uiContract":"^6.0.0"}]}}}`)
	writeFile(t, root, "extensions/plugins/cn.vastplan.foundation.frontend.test/frontend/src/index.ts", `export const value = { uiContract: uiContractVersion };`)
	writeFile(t, root, "examples/plugins/.keep/vastplan.plugin.json", `{}`)

	changes, err := SyncContracts(root, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		if change.Path == "engineering/deploy/portal-platform-catalog.json" {
			t.Fatalf("兼容 minor 不得重写 Portal Profile: %+v", changes)
		}
	}
	catalog, err := os.ReadFile(filepath.Join(root, "engineering/deploy/portal-platform-catalog.json"))
	if err != nil || string(catalog) != `{"uiContract":"^6.0.0"}` {
		t.Fatalf("兼容 Profile 范围被改写: %s err=%v", catalog, err)
	}
}

func TestSyncContractsRejectsIncompatiblePluginClaim(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ContractRegistryPath, "schemaVersion: 1\ncontracts:\n  frontend.ui:\n    version: 6.0.0\n    compatibility: ^6.0.0\n    description: test\n")
	writeFile(t, root, "engineering/deploy/portal-platform-catalog.json", `{"uiContract":"^6.0.0"}`)
	writeFile(t, root, "extensions/plugins/cn.vastplan.feature/vastplan.plugin.json", `{"contributes":{"frontend":{"views":[{"uiContract":"^5.0.0"}]}}}`)
	writeFile(t, root, "examples/plugins/.keep/vastplan.plugin.json", `{}`)
	if _, err := SyncContracts(root, true); err == nil || !strings.Contains(err.Error(), "尚未声明兼容") {
		t.Fatalf("不兼容插件必须阻断: %v", err)
	}
}

func writeFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
