// Package controller schedules and converges append-only artifact security
// rescans as a fenced leader without holding scanner signing material.
package controller

import (
	"errors"
	"slices"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

const (
	PluginID      = artifactassessment.AssessmentControllerPluginID
	PluginVersion = "0.1.0"
	Capability    = "platform.artifacts.assessment.controller"
)

type Config struct {
	TenantID         string   `json:"tenantId"`
	Channels         []string `json:"channels"`
	IntervalSeconds  int      `json:"intervalSeconds"`
	LeadTimeSeconds  int      `json:"leadTimeSeconds"`
	JitterSeconds    int      `json:"jitterSeconds"`
	RetryBaseSeconds int      `json:"retryBaseSeconds"`
	RetryMaxSeconds  int      `json:"retryMaxSeconds"`
	PageSize         int      `json:"pageSize"`
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.TenantID) != c.TenantID || c.TenantID == "" || len(c.TenantID) > 160 || len(c.Channels) == 0 || len(c.Channels) > 16 {
		return errors.New("Assessment Controller tenant/channels 无效")
	}
	for index, channel := range c.Channels {
		if channel == "" || strings.TrimSpace(channel) != channel || slices.Contains(c.Channels[:index], channel) {
			return errors.New("Assessment Controller channels 必须规范且不重复")
		}
	}
	if c.IntervalSeconds < 10 || c.IntervalSeconds > 3600 || c.LeadTimeSeconds < 60 || c.LeadTimeSeconds > 30*24*3600 || c.JitterSeconds < 0 || c.JitterSeconds > c.LeadTimeSeconds/2 || c.RetryBaseSeconds < 1 || c.RetryMaxSeconds < c.RetryBaseSeconds || c.RetryMaxSeconds > 24*3600 || c.PageSize < 1 || c.PageSize > 500 {
		return errors.New("Assessment Controller 调度参数无效")
	}
	return nil
}

func (c Config) Interval() time.Duration  { return time.Duration(c.IntervalSeconds) * time.Second }
func (c Config) LeadTime() time.Duration  { return time.Duration(c.LeadTimeSeconds) * time.Second }
func (c Config) Jitter() time.Duration    { return time.Duration(c.JitterSeconds) * time.Second }
func (c Config) RetryBase() time.Duration { return time.Duration(c.RetryBaseSeconds) * time.Second }
func (c Config) RetryMax() time.Duration  { return time.Duration(c.RetryMaxSeconds) * time.Second }
