package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const AutoscalingMetricMaxAge = 2 * time.Minute

type AutoscalingMetric struct {
	SchemaVersion int       `json:"schema_version"`
	Tenant        string    `json:"tenant,omitempty"`
	Deployment    string    `json:"deployment"`
	Unit          string    `json:"unit"`
	Metric        string    `json:"metric"`
	Value         float64   `json:"value"`
	ObservedAt    time.Time `json:"observed_at"`
}

// PublishAutoscalingMetric 写入短租约指标。KV TTL 让停止上报的监控适配器自动失效，
// 控制器不会用无限陈旧的数据继续扩缩容。
func PublishAutoscalingMetric(ctx context.Context, kv jetstream.KeyValue, metric AutoscalingMetric) error {
	if kv == nil || metric.Deployment == "" || metric.Unit == "" || metric.Metric == "" {
		return errors.New("自动伸缩指标 KV、deployment、unit 和 metric 必须配置")
	}
	if math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) || metric.Value < 0 {
		return errors.New("自动伸缩指标 value 必须是非负有限数")
	}
	metric.SchemaVersion = 1
	if metric.ObservedAt.IsZero() {
		metric.ObservedAt = time.Now().UTC()
	}
	raw, err := json.Marshal(metric)
	if err != nil {
		return err
	}
	key := AutoscalingMetricKey(metric.Tenant, metric.Deployment, metric.Unit, metric.Metric)
	if _, err := kv.Put(ctx, key, raw); err != nil {
		return fmt.Errorf("发布自动伸缩指标: %w", err)
	}
	return nil
}

func ReadAutoscalingMetric(ctx context.Context, kv jetstream.KeyValue, tenant, deployment, unit, name string) (AutoscalingMetric, error) {
	if kv == nil {
		return AutoscalingMetric{}, errors.New("自动伸缩指标 KV 未配置")
	}
	entry, err := kv.Get(ctx, AutoscalingMetricKey(tenant, deployment, unit, name))
	if err != nil {
		return AutoscalingMetric{}, fmt.Errorf("读取自动伸缩指标 %s/%s: %w", unit, name, err)
	}
	var metric AutoscalingMetric
	if err := json.Unmarshal(entry.Value(), &metric); err != nil || metric.SchemaVersion != 1 || math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) || metric.Value < 0 {
		return AutoscalingMetric{}, fmt.Errorf("自动伸缩指标 %s/%s 无效", unit, name)
	}
	if metric.Tenant != tenant || metric.Deployment != deployment || metric.Unit != unit || metric.Metric != name {
		return AutoscalingMetric{}, fmt.Errorf("自动伸缩指标 %s/%s 与 KV key 绑定不一致", unit, name)
	}
	now := time.Now()
	if metric.ObservedAt.IsZero() || metric.ObservedAt.Before(now.Add(-AutoscalingMetricMaxAge)) || metric.ObservedAt.After(now.Add(30*time.Second)) {
		return AutoscalingMetric{}, fmt.Errorf("自动伸缩指标 %s/%s 已过期或时间戳超前", unit, name)
	}
	return metric, nil
}
