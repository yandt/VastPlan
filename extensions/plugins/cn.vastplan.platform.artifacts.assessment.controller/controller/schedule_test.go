package controller

import (
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestScheduleJitterOnlyMovesBeforeSafetyBoundaryAndIsStable(t *testing.T) {
	ref := pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.demo", Version: "1.0.0", Channel: "stable"}
	expires := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	boundary := expires.Add(-24 * time.Hour)
	first := scheduledAt(ref, expires, 24*time.Hour, time.Hour)
	second := scheduledAt(ref, expires, 24*time.Hour, time.Hour)
	if first != second || first.After(boundary) || first.Before(boundary.Add(-time.Hour)) {
		t.Fatalf("稳定抖动越界: %s", first)
	}
}

func TestRetryBackoffIsStableAndBounded(t *testing.T) {
	ref := pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.demo", Version: "1.0.0", Channel: "stable"}
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	first := retryAt(ref, now, 1, 30*time.Second, time.Hour)
	late := retryAt(ref, now, 31, 30*time.Second, time.Hour)
	if first.Before(now.Add(30*time.Second)) || first.After(now.Add(time.Minute)) || late.After(now.Add(time.Hour+30*time.Second)) {
		t.Fatalf("退避未有界: first=%s late=%s", first, late)
	}
}
