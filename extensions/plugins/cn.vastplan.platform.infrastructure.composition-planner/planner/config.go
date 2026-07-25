package planner

import (
	"errors"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

type Config struct {
	Channel                 string   `json:"channel"`
	KernelVersion           string   `json:"kernelVersion"`
	Platform                string   `json:"platform,omitempty"`
	AllowedChannels         []string `json:"allowedChannels"`
	AllowedPublishers       []string `json:"allowedPublishers"`
	AllowedPluginPrefixes   []string `json:"allowedPluginPrefixes,omitempty"`
	AllowDevelopmentPlugins bool     `json:"allowDevelopmentPlugins,omitempty"`
}

func (c Config) Normalize() (Config, error) {
	if c.Channel == "" {
		return Config{}, errors.New("Composition Planner 必须配置自身精确 channel")
	}
	if _, err := semver.StrictNewVersion(c.KernelVersion); err != nil {
		return Config{}, errors.New("Composition Planner kernelVersion 必须是精确 SemVer")
	}
	if len(c.AllowedChannels) == 0 || len(c.AllowedPublishers) == 0 {
		return Config{}, errors.New("Composition Planner 必须配置允许的 channel 和 publisher")
	}
	if c.Platform != "" {
		parts := strings.Split(c.Platform, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return Config{}, errors.New("Composition Planner platform 必须为 os/arch")
		}
	}
	if duplicate(c.AllowedChannels) || duplicate(c.AllowedPublishers) || duplicate(c.AllowedPluginPrefixes) {
		return Config{}, errors.New("Composition Planner 策略列表不得包含重复项")
	}
	c.AllowedChannels = append([]string(nil), c.AllowedChannels...)
	c.AllowedPublishers = append([]string(nil), c.AllowedPublishers...)
	c.AllowedPluginPrefixes = append([]string(nil), c.AllowedPluginPrefixes...)
	// channel 顺序决定仓库选择优先级，不能排序；其余集合按规范顺序绑定摘要。
	sort.Strings(c.AllowedPublishers)
	sort.Strings(c.AllowedPluginPrefixes)
	return c, nil
}

func (c Config) Digest() string { return compositioncommonv1.Digest(c) }

func duplicate(values []string) bool {
	seen := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}
