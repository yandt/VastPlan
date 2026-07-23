package assessmentprovider

import (
	"path/filepath"
	"strings"
	"testing"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
)

func TestConfigRequiresSnapshotPathBoundToDatabaseRevision(t *testing.T) {
	root := t.TempDir()
	revision := strings.Repeat("a", 64)
	config := Config{
		ProviderID: "provider", KeyID: "key",
		SigningKeyRef: commonv1.ManagedCredentialRef{Handle: "credential://managed/assessment", Scope: "tenant", Owner: PluginID, Purpose: SigningPurpose, Version: 1},
		TrivyBinary:   "/usr/local/bin/trivy", TrivySnapshotDirectory: filepath.Join(root, "snapshots", revision), ScannerVersion: "1", DatabaseRevision: revision,
		WorkRoot: filepath.Join(root, "work"), ReportArchiveDirectory: filepath.Join(root, "reports"), TTLHours: 24, TimeoutSeconds: 60,
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("精确 snapshot 路径应通过: %v", err)
	}
	config.TrivySnapshotDirectory = filepath.Join(root, "current")
	if err := config.Validate(); err == nil {
		t.Fatal("可变或未绑定 revision 的 snapshot 路径必须拒绝")
	}
}
