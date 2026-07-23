package sharedstate

import (
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/observability"
)

func TestCapacityPolicyAndLevels(t *testing.T) {
	policy := CapacityPolicy{MaxBytes: 100 << 20, WarningPercent: 70, CriticalPercent: 85}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1_784_000_000, 0).UTC()
	cases := []struct {
		used  uint64
		level CapacityLevel
	}{
		{used: 69 << 20, level: CapacityReady},
		{used: 70 << 20, level: CapacityWarning},
		{used: 85 << 20, level: CapacityCritical},
		{used: 100 << 20, level: CapacityFull},
	}
	for _, item := range cases {
		snapshot := buildCapacitySnapshot(policy, item.used, 12, 64, "JetStream", true, when)
		if snapshot.Level != item.level || snapshot.ObservedAt != when || snapshot.HistoryValues != 12 || snapshot.HistoryPerKey != 64 {
			t.Fatalf("capacity snapshot=%+v", snapshot)
		}
	}
	for _, invalid := range []CapacityPolicy{
		{MaxBytes: MinimumMaxBytes - 1, WarningPercent: 70, CriticalPercent: 85},
		{MaxBytes: 100 << 20, WarningPercent: 85, CriticalPercent: 70},
		{MaxBytes: 100 << 20, WarningPercent: 70, CriticalPercent: 100},
	} {
		if invalid.Validate() == nil {
			t.Fatalf("invalid policy accepted: %+v", invalid)
		}
	}
}

func TestResolveCapacityPolicySeparatesProductionAndDevelopment(t *testing.T) {
	if _, err := ResolveCapacityPolicy(0, 70, 85, false); err == nil {
		t.Fatal("生产容量不得使用隐式默认值")
	}
	development, err := ResolveCapacityPolicy(0, 70, 85, true)
	if err != nil || development.MaxBytes != DevelopmentMaxBytes {
		t.Fatalf("development policy=%+v err=%v", development, err)
	}
	production, err := ResolveCapacityPolicy(256<<20, 60, 90, false)
	if err != nil || production.MaxBytes != 256<<20 || production.WarningPercent != 60 {
		t.Fatalf("production policy=%+v err=%v", production, err)
	}
}

func TestCapacityMetricsUseOnlyFixedLabels(t *testing.T) {
	metrics := observability.NewMemoryMetrics()
	RecordCapacity(CapacitySnapshot{UsedBytes: 10, MaxBytes: 100, AvailableBytes: 90, UsageBasisPoints: 1000, HistoryValues: 7}, metrics)
	snapshot := metrics.Snapshot()
	if snapshot.Gauges["shared_state_capacity_bytes|kind=used"] != 10 || snapshot.Gauges["shared_state_capacity_bytes|kind=max"] != 100 {
		t.Fatalf("capacity metrics=%+v", snapshot.Gauges)
	}
	for key := range snapshot.Gauges {
		if strings.Contains(key, "tenant") || strings.Contains(key, "plugin") || strings.Contains(key, "key") {
			t.Fatalf("容量指标泄漏业务身份: %s", key)
		}
	}
}
