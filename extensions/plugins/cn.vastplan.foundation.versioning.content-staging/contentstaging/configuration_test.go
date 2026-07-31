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
		Limits:                 LimitConfiguration{MaxFileBytes: 1024, MaxTenantBytes: 2048, MaxTotalBytes: 4096, MaxActiveUploadsPerTenant: 2, MaxLeaseSeconds: 300, MaxPreparedPerTenant: 8, PreparedProtectionSeconds: 3600, TerminalRetentionSeconds: 3600},
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

func TestValidateDataPlaneConfiguration(t *testing.T) {
	valid := &DataPlaneConfiguration{
		Listen: "127.0.0.1:9444", Endpoint: "https://content.internal:9444",
		InstanceID: "content-1", TLSIdentity: "spiffe://vastplan/content/content-1",
		AllowedBrowserOrigins: []string{"https://portal.example.com", "http://127.0.0.1:18080"},
		Exposures:             []DataPlaneExposureBinding{{TenantID: "tenant-a", ExposureID: "dpx_aaaaaaaaaaaaaaaaaaaa"}},
	}
	if err := ValidateDataPlaneConfiguration(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*DataPlaneConfiguration){
		"明文 endpoint":       func(value *DataPlaneConfiguration) { value.Endpoint = "http://content.internal:9444" },
		"带路径 endpoint":      func(value *DataPlaneConfiguration) { value.Endpoint += "/upload" },
		"非 SPIFFE identity": func(value *DataPlaneConfiguration) { value.TLSIdentity = "https://identity.invalid" },
		"缺少 listen":         func(value *DataPlaneConfiguration) { value.Listen = "" },
		"缺少 Exposure 绑定":    func(value *DataPlaneConfiguration) { value.Exposures = nil },
		"非 loopback 明文 Origin": func(value *DataPlaneConfiguration) {
			value.AllowedBrowserOrigins = []string{"http://portal.example.com"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := *valid
			mutate(&candidate)
			if err := ValidateDataPlaneConfiguration(&candidate); err == nil {
				t.Fatal("无效数据面配置未被拒绝")
			}
		})
	}
}
