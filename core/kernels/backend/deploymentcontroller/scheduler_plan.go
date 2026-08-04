package deploymentcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/servicemodel"
)

type scheduleBuilder struct {
	scheduler   Scheduler
	deployment  deploymentv2.Deployment
	graph       map[string][]string
	order       []string
	units       map[string]deploymentv2.ServiceUnit
	nodes       map[string]controlplane.NodeRecord
	assignments map[string]deploymentv1.DesiredState
	available   map[string]controlplane.ResourceCapacity
}

func newScheduleBuilder(ctx context.Context, scheduler Scheduler, deployment deploymentv2.Deployment) (*scheduleBuilder, error) {
	if scheduler.Nodes == nil || scheduler.Assignments == nil {
		return nil, errors.New("scheduler 的节点与 assignment KV 必须配置")
	}
	graph := make(map[string][]string, len(deployment.Units))
	units := make(map[string]deploymentv2.ServiceUnit, len(deployment.Units))
	for _, unit := range deployment.Units {
		graph[unit.ID] = append([]string(nil), unit.DependsOn...)
		units[unit.ID] = unit
	}
	if scheduler.Artifacts != nil {
		if err := scheduler.ContractCache.validate(deployment, graph, scheduler.Artifacts); err != nil {
			return nil, err
		}
	}
	order, err := servicemodel.TopologicalOrder(graph)
	if err != nil {
		return nil, fmt.Errorf("部署依赖图无效: %w", err)
	}
	nodes, err := scheduler.liveNodes(ctx, deployment.Metadata.Tenant, deployment.Metadata.Name)
	if err != nil {
		return nil, err
	}
	assignments := make(map[string]deploymentv1.DesiredState, len(nodes))
	for nodeID := range nodes {
		assignments[nodeID] = deploymentv1.DesiredState{
			Version: 1, Metadata: deploymentv1.Metadata{Name: deployment.Metadata.Name, Tenant: deployment.Metadata.Tenant},
			Units: []deploymentv1.Unit{},
		}
	}
	available := make(map[string]controlplane.ResourceCapacity, len(nodes))
	occupied, err := scheduler.occupiedResources(ctx, controlplane.AssignmentPrefix(deployment.Metadata.Tenant, deployment.Metadata.Name))
	if err != nil {
		return nil, err
	}
	for nodeID, node := range nodes {
		capacity := node.Capacity
		capacity.CPUMillis -= occupied[nodeID].CPUMillis
		capacity.MemoryBytes -= occupied[nodeID].MemoryBytes
		capacity.GPU -= occupied[nodeID].GPU
		available[nodeID] = capacity
	}
	return &scheduleBuilder{
		scheduler: scheduler, deployment: deployment, graph: graph, order: order,
		units: units, nodes: nodes, assignments: assignments, available: available,
	}, nil
}

func (builder *scheduleBuilder) placeUnits(ctx context.Context) error {
	for _, unitID := range builder.order {
		unit := builder.units[unitID]
		if !unit.Enabled {
			continue
		}
		if err := builder.placeUnit(ctx, unit); err != nil {
			return err
		}
	}
	return nil
}

func (builder *scheduleBuilder) placeUnit(ctx context.Context, unit deploymentv2.ServiceUnit) error {
	replicas, err := builder.scheduler.desiredReplicas(ctx, builder.deployment, unit)
	if err != nil {
		return err
	}
	policy := servicemodel.Normalize(servicemodel.Policy{
		InstancePolicy: unit.InstancePolicy, StateModel: unit.StateModel,
		Visibility: unit.Visibility, Routing: unit.Routing, RoutingDomain: unit.RoutingDomain,
	})
	if err := servicemodel.Validate(policy); err != nil {
		return fmt.Errorf("unit %s 运行策略无效: %w", unit.ID, err)
	}
	eligible := eligibleNodes(builder.nodes, builder.available, unit)
	if len(eligible) < replicas {
		if policy.InstancePolicy == servicemodel.PolicyPartitioned && len(eligible) > 0 {
			replicas = len(eligible)
		} else {
			return fmt.Errorf("unit %s 需要 %d 副本，但只有 %d 个节点满足标签、亲和与资源约束", unit.ID, replicas, len(eligible))
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		leftPreference := preferenceScore(builder.nodes[eligible[i]].Labels, unit.Placement)
		rightPreference := preferenceScore(builder.nodes[eligible[j]].Labels, unit.Placement)
		if leftPreference != rightPreference {
			return leftPreference > rightPreference
		}
		left, right := placementScore(unit.ID, eligible[i]), placementScore(unit.ID, eligible[j])
		if left != right {
			return left > right
		}
		return eligible[i] < eligible[j]
	})
	selected := eligible[:replicas]
	partitionAssignments := assignPartitions(unit, selected)
	for _, nodeID := range selected {
		builder.assignUnit(nodeID, unit, policy, partitionAssignments[nodeID])
	}
	return nil
}

func (builder *scheduleBuilder) assignUnit(nodeID string, unit deploymentv2.ServiceUnit, policy servicemodel.Policy, partitionKeys []string) {
	if policy.InstancePolicy == servicemodel.PolicyPartitioned && len(partitionKeys) == 0 {
		return
	}
	config := cloneConfig(unit.Config)
	if policy.InstancePolicy == servicemodel.PolicyPartitioned {
		if config == nil {
			config = map[string]any{}
		}
		config["partition_keys"] = append([]string(nil), partitionKeys...)
	}
	startupTier := unit.StartupTier
	if startupTier == "" {
		startupTier = "full"
	}
	clusterMaxReplicas := unit.Replicas
	if unit.Autoscaling != nil {
		clusterMaxReplicas = unit.Autoscaling.MaxReplicas
	}
	assignment := builder.assignments[nodeID]
	assignment.Units = append(assignment.Units, deploymentv1.Unit{
		ID: unit.ID, Kind: unit.Kind, Plugins: append([]deploymentv1.PluginRef(nil), unit.Plugins...),
		Config: config, Enabled: true, ServiceRole: unit.ServiceRole, LogicalService: unit.LogicalService,
		InstancePolicy: policy.InstancePolicy, StateModel: policy.StateModel, Visibility: policy.Visibility,
		Routing: policy.Routing, RoutingDomain: policy.RoutingDomain, StartupTier: startupTier, Replicas: 1,
		ClusterMaxReplicas: clusterMaxReplicas,
		DependsOn:          effectiveUnitDependencies(builder.graph, unit.ID),
		Resources: deploymentv1.ResourceRequirements{Requests: deploymentv1.ResourceList{
			CPUMillis: unit.Resources.Requests.CPUMillis, MemoryBytes: unit.Resources.Requests.MemoryBytes, GPU: unit.Resources.Requests.GPU,
		}},
	})
	builder.assignments[nodeID] = assignment
	capacity := builder.available[nodeID]
	capacity.CPUMillis -= unit.Resources.Requests.CPUMillis
	capacity.MemoryBytes -= unit.Resources.Requests.MemoryBytes
	capacity.GPU -= unit.Resources.Requests.GPU
	builder.available[nodeID] = capacity
}

func (builder *scheduleBuilder) localizeDependencies() {
	for nodeID, assignment := range builder.assignments {
		localUnits := make(map[string]struct{}, len(assignment.Units))
		for _, unit := range assignment.Units {
			localUnits[unit.ID] = struct{}{}
		}
		for index := range assignment.Units {
			localDependencies := assignment.Units[index].DependsOn[:0]
			for _, dependency := range assignment.Units[index].DependsOn {
				if _, local := localUnits[dependency]; local {
					localDependencies = append(localDependencies, dependency)
				}
			}
			assignment.Units[index].DependsOn = localDependencies
		}
		sort.Slice(assignment.Units, func(i, j int) bool { return assignment.Units[i].ID < assignment.Units[j].ID })
		builder.assignments[nodeID] = assignment
	}
}

func (builder *scheduleBuilder) publish(ctx context.Context) (Plan, error) {
	deployment := builder.deployment
	prefix := controlplane.AssignmentPrefix(deployment.Metadata.Tenant, deployment.Metadata.Name)
	existing, maxGeneration, err := builder.scheduler.existingAssignments(ctx, deployment.Metadata.Tenant, deployment.Metadata.Name)
	if err != nil {
		return Plan{}, err
	}
	scheduleGeneration, err := builder.scheduler.scheduleGeneration(ctx, deployment.Metadata.Tenant, deployment.Metadata.Name)
	if err != nil {
		return Plan{}, err
	}
	if scheduleGeneration > maxGeneration {
		maxGeneration = scheduleGeneration
	}
	if assignmentsEqual(builder.assignments, existing) {
		for nodeID, assignment := range builder.assignments {
			assignment.Revision = maxGeneration
			builder.assignments[nodeID] = assignment
		}
		return Plan{Generation: maxGeneration, Assignments: builder.assignments}, nil
	}
	generation, err := builder.scheduler.reserveGeneration(ctx, deployment, maxGeneration)
	if err != nil {
		return Plan{}, err
	}
	for nodeID, assignment := range builder.assignments {
		assignment.Revision = generation
		raw, err := json.Marshal(assignment)
		if err != nil {
			return Plan{}, err
		}
		key := controlplane.AssignmentKey(deployment.Metadata.Tenant, deployment.Metadata.Name, nodeID)
		if _, _, err := controlplane.ApplyDesiredState(ctx, builder.scheduler.Assignments, key, raw); err != nil {
			return Plan{}, fmt.Errorf("发布节点 %s assignment: %w", nodeID, err)
		}
		builder.assignments[nodeID] = assignment
	}
	for key, item := range existing {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if _, live := builder.assignments[item.NodeID]; live {
			continue
		}
		if err := builder.scheduler.Assignments.Delete(ctx, key); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
			return Plan{}, fmt.Errorf("删除离线节点 assignment %s: %w", key, err)
		}
	}
	return Plan{Generation: generation, Assignments: builder.assignments}, nil
}
