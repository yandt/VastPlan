// Package marketplace implements governed multi-source plugin discovery without owning artifact trust or installation state.
package marketplace

import (
	"errors"
	"net"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	commonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/common/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/credentiallease"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginmarketplace"
)

const (
	PluginID       = "cn.vastplan.platform.artifacts.marketplace"
	PluginVersion  = "0.2.1"
	TokenPurpose   = "artifact.marketplace.read-token"
	defaultTimeout = 8 * time.Second
)

type Config struct {
	Sources []SourceConfig `json:"sources"`
}

type SourceConfig struct {
	ID                    string                         `json:"id"`
	Label                 string                         `json:"label"`
	URL                   string                         `json:"url"`
	Priority              int                            `json:"priority"`
	AllowInsecureLoopback bool                           `json:"allowInsecureLoopback,omitempty"`
	CredentialRef         *commonv1.ManagedCredentialRef `json:"credentialRef,omitempty"`
	TimeoutSeconds        int                            `json:"timeoutSeconds,omitempty"`
}

func (c Config) Validate() error {
	if len(c.Sources) == 0 || len(c.Sources) > 32 {
		return errors.New("Marketplace 必须配置 1 至 32 个市场")
	}
	seen := map[string]struct{}{}
	for _, source := range c.Sources {
		if err := source.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[source.ID]; duplicate {
			return errors.New("Marketplace source id 重复: " + source.ID)
		}
		seen[source.ID] = struct{}{}
	}
	return nil
}

func (c Config) normalized() []SourceConfig {
	result := append([]SourceConfig(nil), c.Sources...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority == result[j].Priority {
			return result[i].ID < result[j].ID
		}
		return result[i].Priority < result[j].Priority
	})
	return result
}

func (s SourceConfig) Validate() error {
	if !namePattern.MatchString(s.ID) || strings.TrimSpace(s.Label) == "" || len(s.Label) > 160 || s.Priority < 0 || s.Priority > 10000 || s.TimeoutSeconds < 0 || s.TimeoutSeconds > 60 {
		return errors.New("Marketplace source 基本字段无效: " + s.ID)
	}
	parsed, err := url.Parse(s.URL)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" || parsed.RawPath != "" || strings.Contains(parsed.Path, "..") || (parsed.Path != "" && parsed.Path != "/" && path.Clean(parsed.Path) != strings.TrimRight(parsed.Path, "/")) {
		return errors.New("Marketplace source URL 必须是无凭证、无查询参数的规范 URL: " + s.ID)
	}
	if parsed.Scheme == "vastplan" {
		if parsed.Host != "platform.artifacts.repository" || parsed.Path != "" || s.CredentialRef != nil || s.AllowInsecureLoopback {
			return errors.New("Marketplace 内部 Catalog URL 无效: " + s.ID)
		}
		return nil
	}
	if parsed.Scheme != "https" {
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		loopback := strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
		if parsed.Scheme != "http" || !s.AllowInsecureLoopback || !loopback {
			return errors.New("Marketplace source 生产地址必须使用 HTTPS: " + s.ID)
		}
	}
	if s.CredentialRef != nil {
		ref := *s.CredentialRef
		if ref.Owner != PluginID || ref.Purpose != TokenPurpose || ref.Scope != "tenant" || ref.Name != "" || credentiallease.ValidateCredentialRef(ref) != nil {
			return errors.New("Marketplace source credentialRef 必须绑定当前插件和租户: " + s.ID)
		}
	}
	return nil
}

func (s SourceConfig) Public() pluginmarketplace.Source {
	return pluginmarketplace.Source{ID: s.ID, Label: s.Label, URL: strings.TrimRight(s.URL, "/"), Priority: s.Priority}
}

func (s SourceConfig) Timeout() time.Duration {
	if s.TimeoutSeconds == 0 {
		return defaultTimeout
	}
	return time.Duration(s.TimeoutSeconds) * time.Second
}
