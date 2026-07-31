package contentstaging

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const FileProviderProtocol = "version.staging.storage.file.v1"

type StartupConfiguration struct {
	Provider               ProviderConfiguration `json:"provider"`
	Limits                 LimitConfiguration    `json:"limits"`
	ReclaimIntervalSeconds int                   `json:"reclaimIntervalSeconds"`
}

type ProviderConfiguration struct {
	Protocol string `json:"protocol"`
	Root     string `json:"root"`
}

type LimitConfiguration struct {
	MaxFileBytes              int64 `json:"maxFileBytes"`
	MaxTenantBytes            int64 `json:"maxTenantBytes"`
	MaxTotalBytes             int64 `json:"maxTotalBytes"`
	MaxActiveUploadsPerTenant int   `json:"maxActiveUploadsPerTenant"`
	MaxLeaseSeconds           int   `json:"maxLeaseSeconds"`
	TerminalRetentionSeconds  int   `json:"terminalRetentionSeconds"`
}

func BuildConfiguredService(ctx context.Context, configuration StartupConfiguration) (*Service, time.Duration, error) {
	if configuration.Provider.Protocol != FileProviderProtocol {
		return nil, 0, fmt.Errorf("P2.4b 尚不支持 Content Staging Provider protocol %q", configuration.Provider.Protocol)
	}
	if configuration.ReclaimIntervalSeconds < 5 || configuration.ReclaimIntervalSeconds > 3600 {
		return nil, 0, errors.New("Content Staging 回收周期必须为 5 到 3600 秒")
	}
	limits := Limits{
		MaxFileBytes: configuration.Limits.MaxFileBytes, MaxTenantBytes: configuration.Limits.MaxTenantBytes,
		MaxTotalBytes: configuration.Limits.MaxTotalBytes, MaxActiveUploadsPerTenant: configuration.Limits.MaxActiveUploadsPerTenant,
		MaxLeaseSeconds:   configuration.Limits.MaxLeaseSeconds,
		TerminalRetention: time.Duration(configuration.Limits.TerminalRetentionSeconds) * time.Second,
	}
	if configuration.Limits.TerminalRetentionSeconds < 60 {
		return nil, 0, errors.New("Content Staging 终态至少保留 60 秒")
	}
	provider, err := OpenFileProvider(configuration.Provider.Root)
	if err != nil {
		return nil, 0, fmt.Errorf("打开 Content Staging File Provider: %w", err)
	}
	manager, err := NewManager(ctx, provider, IntegrityAdmission{}, ManagerOptions{Limits: limits})
	if err != nil {
		return nil, 0, err
	}
	return NewService(manager), time.Duration(configuration.ReclaimIntervalSeconds) * time.Second, nil
}
