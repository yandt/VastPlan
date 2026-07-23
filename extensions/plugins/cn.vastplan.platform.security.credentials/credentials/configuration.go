package credentials

import (
	"errors"
	"time"
)

const (
	defaultPreparingMaxAge   = 24 * time.Hour
	defaultAbortedRetention  = 30 * 24 * time.Hour
	defaultAuditRetention    = 180 * 24 * time.Hour
	defaultMaintenancePeriod = time.Hour
	defaultMaintenanceBatch  = 200
)

type Configuration struct {
	Maintenance MaintenanceConfiguration `json:"maintenance,omitempty"`
}

type MaintenanceConfiguration struct {
	PreparingMaxAgeSeconds  int64 `json:"preparingMaxAgeSeconds,omitempty"`
	AbortedRetentionSeconds int64 `json:"abortedRetentionSeconds,omitempty"`
	AuditRetentionSeconds   int64 `json:"auditRetentionSeconds,omitempty"`
	IntervalSeconds         int64 `json:"intervalSeconds,omitempty"`
	BatchSize               int   `json:"batchSize,omitempty"`
}

type MaintenancePolicy struct {
	PreparingMaxAge  time.Duration
	AbortedRetention time.Duration
	AuditRetention   time.Duration
	Interval         time.Duration
	BatchSize        int
}

func (configuration Configuration) Policy() (MaintenancePolicy, error) {
	value := configuration.Maintenance
	if !secondsWithin(value.PreparingMaxAgeSeconds, 300, 604800) ||
		!secondsWithin(value.AbortedRetentionSeconds, 3600, 31536000) ||
		!secondsWithin(value.AuditRetentionSeconds, 3600, 63072000) ||
		!secondsWithin(value.IntervalSeconds, 60, 86400) || value.BatchSize < 0 || value.BatchSize > 1000 {
		return MaintenancePolicy{}, errors.New("凭证维护配置超出安全范围")
	}
	policy := MaintenancePolicy{
		PreparingMaxAge:  durationOrDefault(value.PreparingMaxAgeSeconds, defaultPreparingMaxAge),
		AbortedRetention: durationOrDefault(value.AbortedRetentionSeconds, defaultAbortedRetention),
		AuditRetention:   durationOrDefault(value.AuditRetentionSeconds, defaultAuditRetention),
		Interval:         durationOrDefault(value.IntervalSeconds, defaultMaintenancePeriod),
		BatchSize:        value.BatchSize,
	}
	if policy.BatchSize == 0 {
		policy.BatchSize = defaultMaintenanceBatch
	}
	if err := validateMaintenancePolicy(policy); err != nil {
		return MaintenancePolicy{}, err
	}
	return policy, nil
}

func validateMaintenancePolicy(policy MaintenancePolicy) error {
	if policy.PreparingMaxAge < 5*time.Minute || policy.PreparingMaxAge > 7*24*time.Hour ||
		policy.AbortedRetention < time.Hour || policy.AbortedRetention > 365*24*time.Hour ||
		policy.AuditRetention < policy.AbortedRetention || policy.AuditRetention > 730*24*time.Hour ||
		policy.Interval < time.Minute || policy.Interval > 24*time.Hour || policy.BatchSize < 1 || policy.BatchSize > 1000 {
		return errors.New("凭证维护配置超出安全范围")
	}
	return nil
}

func secondsWithin(value, minimum, maximum int64) bool {
	return value == 0 || (value >= minimum && value <= maximum)
}

func durationOrDefault(seconds int64, fallback time.Duration) time.Duration {
	if seconds == 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
