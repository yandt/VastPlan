// Package observability 提供 Backend Kernel 不绑定具体后端的可观测内核。
// 日志使用标准库 slog；指标通过窄接口输出；trace 身份沿 CallContext 传播。
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

type MetricSink interface {
	AddCounter(name string, delta int64, labels map[string]string)
	ObserveDuration(name string, duration time.Duration, labels map[string]string)
	SetGauge(name string, value int64, labels map[string]string)
}

type Snapshot struct {
	Counters  map[string]int64         `json:"counters"`
	Gauges    map[string]int64         `json:"gauges"`
	Durations map[string]DurationStats `json:"durations"`
}

type DurationStats struct {
	Count int64         `json:"count"`
	Total time.Duration `json:"total"`
	Max   time.Duration `json:"max"`
}

// MemoryMetrics 是有界、进程内诊断 sink。标签只能由内核固定集合提供，不能放用户 ID。
type MemoryMetrics struct {
	mu        sync.RWMutex
	counters  map[string]int64
	gauges    map[string]int64
	durations map[string]DurationStats
}

func NewMemoryMetrics() *MemoryMetrics {
	return &MemoryMetrics{counters: map[string]int64{}, gauges: map[string]int64{}, durations: map[string]DurationStats{}}
}

func metricKey(name string, labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString(name)
	for _, key := range keys {
		out.WriteString("|")
		out.WriteString(key)
		out.WriteString("=")
		out.WriteString(labels[key])
	}
	return out.String()
}

func (m *MemoryMetrics) AddCounter(name string, delta int64, labels map[string]string) {
	m.mu.Lock()
	m.counters[metricKey(name, labels)] += delta
	m.mu.Unlock()
}
func (m *MemoryMetrics) SetGauge(name string, value int64, labels map[string]string) {
	m.mu.Lock()
	m.gauges[metricKey(name, labels)] = value
	m.mu.Unlock()
}
func (m *MemoryMetrics) ObserveDuration(name string, duration time.Duration, labels map[string]string) {
	key := metricKey(name, labels)
	m.mu.Lock()
	stats := m.durations[key]
	stats.Count++
	stats.Total += duration
	if duration > stats.Max {
		stats.Max = duration
	}
	m.durations[key] = stats
	m.mu.Unlock()
}
func (m *MemoryMetrics) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := Snapshot{Counters: map[string]int64{}, Gauges: map[string]int64{}, Durations: map[string]DurationStats{}}
	for k, v := range m.counters {
		out.Counters[k] = v
	}
	for k, v := range m.gauges {
		out.Gauges[k] = v
	}
	for k, v := range m.durations {
		out.Durations[k] = v
	}
	return out
}

type Observer struct {
	Logger  *slog.Logger
	Metrics MetricSink
}

func New(logger *slog.Logger, metrics MetricSink) *Observer {
	if logger == nil {
		logger = slog.Default()
	}
	if metrics == nil {
		metrics = NewMemoryMetrics()
	}
	return &Observer{Logger: logger, Metrics: metrics}
}

// BeginCall 派生新 span 并返回结束函数。trace_id 保持不变，旧 span_id 成为 parent。
func (o *Observer) BeginCall(ctx context.Context, callCtx *contractv1.CallContext, operation string, labels map[string]string) (*contractv1.CallContext, func(status string, err error)) {
	started := time.Now()
	bounded := &contractv1.CallContext{}
	if callCtx != nil {
		bounded = proto.Clone(callCtx).(*contractv1.CallContext)
	}
	trace := bounded.Trace
	if trace == nil {
		trace = &contractv1.Trace{}
	} else {
		trace = proto.Clone(trace).(*contractv1.Trace)
	}
	if trace.TraceId == "" {
		trace.TraceId = randomID(16)
	}
	parentSpanID := trace.SpanId
	if parentSpanID != "" {
		trace.ParentSpanId = &parentSpanID
	} else {
		trace.ParentSpanId = nil
	}
	trace.SpanId = randomID(8)
	bounded.Trace = trace
	attrs := []any{"operation", operation, "trace_id", trace.TraceId, "span_id", trace.SpanId}
	for key, value := range labels {
		attrs = append(attrs, key, value)
	}
	o.Logger.Log(ctx, slog.LevelDebug, "kernel call started", attrs...)
	return bounded, func(status string, err error) {
		metricLabels := map[string]string{"operation": operation, "status": status}
		o.Metrics.AddCounter("kernel_calls_total", 1, metricLabels)
		o.Metrics.ObserveDuration("kernel_call_duration", time.Since(started), metricLabels)
		endAttrs := append(append([]any{}, attrs...), "status", status, "duration_ms", time.Since(started).Milliseconds())
		if err != nil {
			endAttrs = append(endAttrs, "error", err.Error())
		}
		// 成功调用在高频健康检查、门户预取等路径中十分常见；保留完整
		// trace/metrics，但避免把每一次正常完成都写入生产 INFO 日志。
		level := slog.LevelDebug
		if err != nil || status != "STATUS_OK" {
			level = slog.LevelWarn
		}
		o.Logger.Log(ctx, level, "kernel call finished", endAttrs...)
	}
}

func randomID(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b)
}

func (o *Observer) Snapshot() Snapshot {
	if metrics, ok := o.Metrics.(*MemoryMetrics); ok {
		return metrics.Snapshot()
	}
	return Snapshot{}
}
