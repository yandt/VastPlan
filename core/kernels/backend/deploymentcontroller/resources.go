package deploymentcontroller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"github.com/nats-io/nats.go/jetstream"
)

func effectiveUnitDependencies(graph map[string][]string, unitID string) []string {
	dependencies := append([]string(nil), graph[unitID]...)
	sort.Strings(dependencies)
	return dependencies
}

func (s Scheduler) occupiedResources(ctx context.Context, currentPrefix string) (map[string]controlplane.ResourceCapacity, error) {
	keys, err := s.Assignments.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return map[string]controlplane.ResourceCapacity{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("列出资源占用 assignment: %w", err)
	}
	occupied := map[string]controlplane.ResourceCapacity{}
	for _, key := range keys {
		if strings.HasPrefix(key, currentPrefix) || strings.HasSuffix(key, ".schedule") {
			continue
		}
		entry, err := s.Assignments.Get(ctx, key)
		if err != nil {
			continue
		}
		state, err := deploymentv1.Parse(entry.Value())
		if err != nil {
			return nil, fmt.Errorf("资源占用 assignment %s 损坏: %w", key, err)
		}
		nodeID, err := controlplane.AssignmentKeyNodeID(key)
		if err != nil {
			return nil, err
		}
		capacity := occupied[nodeID]
		for _, unit := range state.Units {
			capacity.CPUMillis += unit.Resources.Requests.CPUMillis
			capacity.MemoryBytes += unit.Resources.Requests.MemoryBytes
			capacity.GPU += unit.Resources.Requests.GPU
		}
		occupied[nodeID] = capacity
	}
	return occupied, nil
}

func (s Scheduler) desiredReplicas(ctx context.Context, deployment deploymentv2.Deployment, unit deploymentv2.ServiceUnit) (int, error) {
	if unit.Autoscaling == nil {
		return unit.Replicas, nil
	}
	if unit.Autoscaling.MinReplicas < 1 || unit.Autoscaling.MaxReplicas < unit.Autoscaling.MinReplicas || unit.Autoscaling.TargetValuePerReplica <= 0 || math.IsNaN(unit.Autoscaling.TargetValuePerReplica) || math.IsInf(unit.Autoscaling.TargetValuePerReplica, 0) {
		return 0, fmt.Errorf("unit %s 自动伸缩配置无效", unit.ID)
	}
	metric, err := controlplane.ReadAutoscalingMetric(ctx, s.Metrics, deployment.Metadata.Tenant, deployment.Metadata.Name, unit.ID, unit.Autoscaling.Metric)
	if err != nil {
		return 0, fmt.Errorf("unit %s 自动伸缩 fail-closed: %w", unit.ID, err)
	}
	desired := math.Ceil(metric.Value / unit.Autoscaling.TargetValuePerReplica)
	if desired >= float64(unit.Autoscaling.MaxReplicas) {
		return unit.Autoscaling.MaxReplicas, nil
	}
	replicas := int(desired)
	if replicas < unit.Autoscaling.MinReplicas {
		replicas = unit.Autoscaling.MinReplicas
	}
	if replicas > unit.Autoscaling.MaxReplicas {
		replicas = unit.Autoscaling.MaxReplicas
	}
	return replicas, nil
}
