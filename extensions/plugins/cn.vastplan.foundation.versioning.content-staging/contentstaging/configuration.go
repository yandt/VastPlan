package contentstaging

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

const FileProviderProtocol = "version.staging.storage.file.v1"

var dataPlaneExposureIDPattern = regexp.MustCompile(`^dpx_[a-z2-7]{20}$`)

type StartupConfiguration struct {
	Provider               ProviderConfiguration   `json:"provider"`
	Limits                 LimitConfiguration      `json:"limits"`
	ReclaimIntervalSeconds int                     `json:"reclaimIntervalSeconds"`
	DataPlane              *DataPlaneConfiguration `json:"dataPlane,omitempty"`
}

type DataPlaneConfiguration struct {
	Listen                string                         `json:"listen"`
	Endpoint              string                         `json:"endpoint"`
	InstanceID            string                         `json:"instanceId"`
	TLSIdentity           string                         `json:"tlsIdentity"`
	AllowedBrowserOrigins []string                       `json:"allowedBrowserOrigins,omitempty"`
	Exposures             []DataPlaneExposureBinding     `json:"exposures"`
	Private               *PrivateDataPlaneConfiguration `json:"private,omitempty"`
}

type PrivateDataPlaneConfiguration struct {
	Listen                 string   `json:"listen"`
	Endpoint               string   `json:"endpoint"`
	InstanceID             string   `json:"instanceId"`
	TLSIdentity            string   `json:"tlsIdentity"`
	ClientIdentityPrefixes []string `json:"clientIdentityPrefixes"`
}

type DataPlaneExposureBinding struct {
	TenantID   string `json:"tenantId"`
	ExposureID string `json:"exposureId"`
}

func ValidateDataPlaneConfiguration(configuration *DataPlaneConfiguration) error {
	if configuration == nil {
		return nil
	}
	endpoint, err := url.Parse(configuration.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("Content Upload endpoint 必须是无路径、凭据和 query 的 HTTPS origin")
	}
	identity, err := url.Parse(configuration.TLSIdentity)
	if err != nil || identity.Scheme != "spiffe" || identity.Host == "" || identity.RawQuery != "" || identity.Fragment != "" || strings.Contains(identity.Path, "..") {
		return errors.New("Content Upload tlsIdentity 必须是规范 SPIFFE URI")
	}
	if strings.TrimSpace(configuration.Listen) == "" || strings.TrimSpace(configuration.InstanceID) == "" {
		return errors.New("Content Upload 数据面配置不完整")
	}
	if len(configuration.Exposures) == 0 || len(configuration.Exposures) > 1_000 {
		return errors.New("Content Upload 必须配置 1 到 1000 个 tenant Exposure 绑定")
	}
	tenants, exposures := map[string]struct{}{}, map[string]struct{}{}
	for _, binding := range configuration.Exposures {
		if versioningv1.ValidateVersionIdentityTenant(binding.TenantID) != nil || !dataPlaneExposureIDPattern.MatchString(binding.ExposureID) {
			return errors.New("Content Upload tenant Exposure 绑定无效")
		}
		if _, duplicate := tenants[binding.TenantID]; duplicate {
			return errors.New("Content Upload tenant Exposure 绑定重复")
		}
		if _, duplicate := exposures[binding.ExposureID]; duplicate {
			return errors.New("Content Upload Exposure 不能跨 tenant 复用")
		}
		tenants[binding.TenantID], exposures[binding.ExposureID] = struct{}{}, struct{}{}
	}
	if len(configuration.AllowedBrowserOrigins) > 32 {
		return errors.New("Content Upload 浏览器 Origin 白名单超过 32 项")
	}
	seen := map[string]struct{}{}
	for _, origin := range configuration.AllowedBrowserOrigins {
		if err := validateBrowserOrigin(origin); err != nil {
			return err
		}
		if _, duplicate := seen[origin]; duplicate {
			return errors.New("Content Upload 浏览器 Origin 白名单重复")
		}
		seen[origin] = struct{}{}
	}
	if configuration.Private != nil {
		private := configuration.Private
		endpoint, err := url.Parse(private.Endpoint)
		if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || strings.TrimSpace(private.Listen) == "" || private.InstanceID == "" || private.InstanceID == configuration.InstanceID {
			return errors.New("Content Upload Private 数据面配置无效")
		}
		identity, err := url.Parse(private.TLSIdentity)
		if err != nil || identity.Scheme != "spiffe" || identity.Host == "" || identity.RawQuery != "" || identity.Fragment != "" {
			return errors.New("Content Upload Private tlsIdentity 无效")
		}
		if len(private.ClientIdentityPrefixes) == 0 || len(private.ClientIdentityPrefixes) > 32 {
			return errors.New("Content Upload Private 必须配置客户端 SPIFFE 前缀")
		}
		for _, prefix := range private.ClientIdentityPrefixes {
			parsed, err := url.Parse(prefix)
			if err != nil || parsed.Scheme != "spiffe" || parsed.Host == "" || !strings.HasSuffix(parsed.Path, "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
				return errors.New("Content Upload Private 客户端 SPIFFE 前缀无效")
			}
		}
	}
	return nil
}

func (configuration DataPlaneConfiguration) ExposureForTenant(tenantID string) (string, bool) {
	for _, binding := range configuration.Exposures {
		if binding.TenantID == tenantID {
			return binding.ExposureID, true
		}
	}
	return "", false
}

func validateBrowserOrigin(origin string) error {
	parsed, err := url.Parse(origin)
	loopbackHTTP := parsed != nil && parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1")
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && !loopbackHTTP) || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || origin != strings.ToLower(origin) {
		return errors.New("Content Upload 浏览器 Origin 必须是规范小写 HTTPS origin；仅 loopback 开发地址可使用 HTTP")
	}
	return nil
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
	MaxPreparedPerTenant      int   `json:"maxPreparedPerTenant"`
	PreparedProtectionSeconds int   `json:"preparedProtectionSeconds"`
	TerminalRetentionSeconds  int   `json:"terminalRetentionSeconds"`
}

func BuildConfiguredService(ctx context.Context, configuration StartupConfiguration) (*Service, time.Duration, error) {
	if err := ValidateDataPlaneConfiguration(configuration.DataPlane); err != nil {
		return nil, 0, err
	}
	if configuration.Provider.Protocol != FileProviderProtocol {
		return nil, 0, fmt.Errorf("P2.4b 尚不支持 Content Staging Provider protocol %q", configuration.Provider.Protocol)
	}
	if configuration.ReclaimIntervalSeconds < 5 || configuration.ReclaimIntervalSeconds > 3600 {
		return nil, 0, errors.New("Content Staging 回收周期必须为 5 到 3600 秒")
	}
	limits := Limits{
		MaxFileBytes: configuration.Limits.MaxFileBytes, MaxTenantBytes: configuration.Limits.MaxTenantBytes,
		MaxTotalBytes: configuration.Limits.MaxTotalBytes, MaxActiveUploadsPerTenant: configuration.Limits.MaxActiveUploadsPerTenant,
		MaxLeaseSeconds: configuration.Limits.MaxLeaseSeconds, MaxPreparedPerTenant: configuration.Limits.MaxPreparedPerTenant,
		PreparedProtection: time.Duration(configuration.Limits.PreparedProtectionSeconds) * time.Second,
		TerminalRetention:  time.Duration(configuration.Limits.TerminalRetentionSeconds) * time.Second,
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
