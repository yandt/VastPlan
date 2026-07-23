// Package otpprovider implements a delivery-neutral enterprise one-time-code authentication method.
package otpprovider

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
)

const (
	PluginID      = "cn.vastplan.platform.security.authentication.method.otp"
	PluginVersion = "0.2.0"
	ProviderID    = "enterprise-one-time-code"
	EmailMethodID = "enterprise-email-code"
	SMSMethodID   = "enterprise-sms-code"
)

var stableID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$`)

type Configuration struct {
	Profiles  map[string]Profile `json:"profiles"`
	Capacity  int                `json:"capacity,omitempty"`
	StateFile string             `json:"stateFile,omitempty"`
}

type Profile struct {
	MethodID          string                           `json:"methodId"`
	DeliveryProfileID string                           `json:"deliveryProfileId"`
	Channel           authenticationv1.DeliveryChannel `json:"channel"`
	Issuer            string                           `json:"issuer"`
	CodeLength        int                              `json:"codeLength,omitempty"`
	TTLSeconds        int                              `json:"ttlSeconds,omitempty"`
	ResendSeconds     int                              `json:"resendSeconds,omitempty"`
	MaxAttempts       int                              `json:"maxAttempts,omitempty"`
	MaxResends        *int                             `json:"maxResends,omitempty"`
	maxResends        int
}

func (c Configuration) normalized() (Configuration, error) {
	if len(c.Profiles) == 0 || len(c.Profiles) > 64 {
		return Configuration{}, errors.New("OTP Provider profiles 必须为 1..64 个")
	}
	if c.Capacity == 0 {
		c.Capacity = 4096
	}
	if c.Capacity < 1 || c.Capacity > 100000 {
		return Configuration{}, errors.New("OTP Provider capacity 必须为 1..100000")
	}
	profiles := make(map[string]Profile, len(c.Profiles))
	for id, profile := range c.Profiles {
		if !stableID.MatchString(id) || !stableID.MatchString(profile.DeliveryProfileID) || profile.Issuer == "" || len(profile.Issuer) > 512 {
			return Configuration{}, fmt.Errorf("OTP Profile %q 标识或 issuer 无效", id)
		}
		if profile.MethodID != EmailMethodID && profile.MethodID != SMSMethodID {
			return Configuration{}, fmt.Errorf("OTP Profile %q methodId 无效", id)
		}
		expected := authenticationv1.DeliveryEmail
		if profile.MethodID == SMSMethodID {
			expected = authenticationv1.DeliverySMS
		}
		if profile.Channel != expected {
			return Configuration{}, fmt.Errorf("OTP Profile %q channel 与 methodId 不一致", id)
		}
		if profile.CodeLength == 0 {
			profile.CodeLength = 6
		}
		if profile.TTLSeconds == 0 {
			profile.TTLSeconds = 300
		}
		if profile.ResendSeconds == 0 {
			profile.ResendSeconds = 60
		}
		if profile.MaxAttempts == 0 {
			profile.MaxAttempts = 5
		}
		profile.maxResends = 3
		if profile.MaxResends != nil {
			profile.maxResends = *profile.MaxResends
		}
		if profile.CodeLength < 4 || profile.CodeLength > 12 || profile.TTLSeconds < 30 || profile.TTLSeconds > 600 || profile.ResendSeconds < 5 || profile.ResendSeconds > profile.TTLSeconds || profile.MaxAttempts < 1 || profile.MaxAttempts > 10 || profile.maxResends < 0 || profile.maxResends > 10 {
			return Configuration{}, fmt.Errorf("OTP Profile %q 安全参数超出边界", id)
		}
		profiles[id] = profile
	}
	c.Profiles = profiles
	return c, nil
}

func (p Profile) ttl() time.Duration         { return time.Duration(p.TTLSeconds) * time.Second }
func (p Profile) resendDelay() time.Duration { return time.Duration(p.ResendSeconds) * time.Second }
