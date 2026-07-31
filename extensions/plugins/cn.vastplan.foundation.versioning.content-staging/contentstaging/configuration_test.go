package contentstaging

import (
	"context"
	"os"
	"testing"
)

func TestBuildConfiguredServiceRequiresBoundedFileProvider(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configuration := StartupConfiguration{
		Provider:               ProviderConfiguration{Protocol: FileProviderProtocol, Root: root},
		Limits:                 LimitConfiguration{MaxFileBytes: 1024, MaxTenantBytes: 2048, MaxTotalBytes: 4096, MaxActiveUploadsPerTenant: 2, MaxLeaseSeconds: 300, TerminalRetentionSeconds: 3600},
		ReclaimIntervalSeconds: 30,
	}
	service, interval, err := BuildConfiguredService(context.Background(), configuration)
	if err != nil || service == nil || interval.Seconds() != 30 {
		t.Fatalf("build: service=%v interval=%v err=%v", service, interval, err)
	}
	configuration.Provider.Protocol = "s3"
	if _, _, err := BuildConfiguredService(context.Background(), configuration); err == nil {
		t.Fatal("未知 Provider protocol 应失败关闭")
	}
}
