package sharedstate

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"cdsoft.com.cn/VastPlan/core/shared/go/observability"
)

const (
	DevelopmentMaxBytes = int64(1 << 30)
	MinimumMaxBytes     = int64(64 << 20)
	MaximumMaxBytes     = int64(1 << 50)

	DefaultWarningPercent  = 70
	DefaultCriticalPercent = 85

	metadataCapacitySchema  = "vastplan.shared-state.capacity.schema"
	metadataWarningPercent  = "vastplan.shared-state.capacity.warning-percent"
	metadataCriticalPercent = "vastplan.shared-state.capacity.critical-percent"
	capacitySchemaVersion   = "1"
)

type CapacityPolicy struct {
	MaxBytes        int64 `json:"maxBytes"`
	WarningPercent  int   `json:"warningPercent"`
	CriticalPercent int   `json:"criticalPercent"`
}

func DevelopmentCapacityPolicy() CapacityPolicy {
	return CapacityPolicy{MaxBytes: DevelopmentMaxBytes, WarningPercent: DefaultWarningPercent, CriticalPercent: DefaultCriticalPercent}
}

func ResolveCapacityPolicy(maxBytes int64, warningPercent, criticalPercent int, allowDevelopmentDefault bool) (CapacityPolicy, error) {
	if maxBytes == 0 {
		if !allowDevelopmentDefault {
			return CapacityPolicy{}, errors.New("生产 bootstrap 必须显式配置 shared-state-max-bytes")
		}
		maxBytes = DevelopmentMaxBytes
	}
	policy := CapacityPolicy{MaxBytes: maxBytes, WarningPercent: warningPercent, CriticalPercent: criticalPercent}
	return policy, policy.Validate()
}

func (policy CapacityPolicy) Validate() error {
	if policy.MaxBytes < MinimumMaxBytes || policy.MaxBytes > MaximumMaxBytes {
		return fmt.Errorf("Shared State max bytes 必须为 %d-%d", MinimumMaxBytes, MaximumMaxBytes)
	}
	if policy.WarningPercent < 1 || policy.WarningPercent >= policy.CriticalPercent || policy.CriticalPercent >= 100 {
		return errors.New("Shared State 容量阈值必须满足 1 <= warning < critical < 100")
	}
	return nil
}

func (policy CapacityPolicy) Metadata() (map[string]string, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return map[string]string{
		metadataCapacitySchema:  capacitySchemaVersion,
		metadataWarningPercent:  strconv.Itoa(policy.WarningPercent),
		metadataCriticalPercent: strconv.Itoa(policy.CriticalPercent),
	}, nil
}

type CapacityLevel string

const (
	CapacityReady    CapacityLevel = "ready"
	CapacityWarning  CapacityLevel = "warning"
	CapacityCritical CapacityLevel = "critical"
	CapacityFull     CapacityLevel = "full"
)

type CapacitySnapshot struct {
	SchemaVersion    int           `json:"schemaVersion"`
	ObservedAt       time.Time     `json:"observedAt"`
	Level            CapacityLevel `json:"level"`
	UsedBytes        uint64        `json:"usedBytes"`
	MaxBytes         uint64        `json:"maxBytes"`
	AvailableBytes   uint64        `json:"availableBytes"`
	UsageBasisPoints uint64        `json:"usageBasisPoints"`
	HistoryValues    uint64        `json:"historyValues"`
	HistoryPerKey    int64         `json:"historyPerKey"`
	BackingStore     string        `json:"backingStore"`
	Compressed       bool          `json:"compressed"`
	WarningPercent   int           `json:"warningPercent"`
	CriticalPercent  int           `json:"criticalPercent"`
}

func InspectCapacity(ctx context.Context, kv jetstream.KeyValue) (CapacitySnapshot, error) {
	if kv == nil {
		return CapacitySnapshot{}, errors.New("Shared State KV 不能为空")
	}
	status, err := kv.Status(ctx)
	if err != nil {
		return CapacitySnapshot{}, fmt.Errorf("读取 Shared State 容量: %w", err)
	}
	config := status.Config()
	policy, err := capacityPolicyFromConfig(config)
	if err != nil {
		return CapacitySnapshot{}, err
	}
	return buildCapacitySnapshot(policy, status.Bytes(), status.Values(), status.History(), status.BackingStore(), status.IsCompressed(), time.Now().UTC()), nil
}

func buildCapacitySnapshot(policy CapacityPolicy, used, values uint64, history int64, backingStore string, compressed bool, observedAt time.Time) CapacitySnapshot {
	maximum := uint64(policy.MaxBytes)
	available := uint64(0)
	if used < maximum {
		available = maximum - used
	}
	basisPoints := uint64(10_000)
	if used < maximum {
		basisPoints = used * 10_000 / maximum
	}
	level := CapacityReady
	switch {
	case used >= maximum:
		level = CapacityFull
	case basisPoints >= uint64(policy.CriticalPercent*100):
		level = CapacityCritical
	case basisPoints >= uint64(policy.WarningPercent*100):
		level = CapacityWarning
	}
	return CapacitySnapshot{
		SchemaVersion: 1, ObservedAt: observedAt, Level: level, UsedBytes: used, MaxBytes: maximum,
		AvailableBytes: available, UsageBasisPoints: basisPoints, HistoryValues: values, HistoryPerKey: history,
		BackingStore: backingStore, Compressed: compressed,
		WarningPercent: policy.WarningPercent, CriticalPercent: policy.CriticalPercent,
	}
}

func RecordCapacity(snapshot CapacitySnapshot, metrics observability.MetricSink) {
	if metrics == nil {
		return
	}
	metrics.SetGauge("shared_state_capacity_bytes", int64(snapshot.UsedBytes), map[string]string{"kind": "used"})
	metrics.SetGauge("shared_state_capacity_bytes", int64(snapshot.MaxBytes), map[string]string{"kind": "max"})
	metrics.SetGauge("shared_state_capacity_bytes", int64(snapshot.AvailableBytes), map[string]string{"kind": "available"})
	metrics.SetGauge("shared_state_capacity_usage_basis_points", int64(snapshot.UsageBasisPoints), nil)
	metrics.SetGauge("shared_state_history_values", int64(snapshot.HistoryValues), nil)
}

func capacityPolicyFromConfig(config jetstream.KeyValueConfig) (CapacityPolicy, error) {
	warning, warningErr := strconv.Atoi(config.Metadata[metadataWarningPercent])
	critical, criticalErr := strconv.Atoi(config.Metadata[metadataCriticalPercent])
	policy := CapacityPolicy{MaxBytes: config.MaxBytes, WarningPercent: warning, CriticalPercent: critical}
	if config.Metadata[metadataCapacitySchema] != capacitySchemaVersion || warningErr != nil || criticalErr != nil || policy.Validate() != nil {
		return CapacityPolicy{}, errors.New("Shared State stream 缺少有效容量策略 metadata 或硬上限")
	}
	return policy, nil
}
