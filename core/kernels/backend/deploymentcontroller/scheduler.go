// Package deploymentcontroller 把全局集群部署规格调度成每节点可执行快照。
//
// 控制器属于 Plugin Service 的期望态职责层；Node Agent 只消费 assignment 并执行，
// 不参与全局副本仲裁。
package deploymentcontroller

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
)

type Scheduler struct {
	Nodes        jetstream.KeyValue
	Assignments  jetstream.KeyValue
	Metrics      jetstream.KeyValue
	Actual       jetstream.KeyValue
	Compositions jetstream.KeyValue
	Artifacts    ArtifactReader
}

type Plan struct {
	Generation  uint64
	Assignments map[string]deploymentv1.DesiredState
}

type scheduleState struct {
	SchemaVersion      int       `json:"schema_version"`
	Generation         uint64    `json:"generation"`
	DeploymentRevision uint64    `json:"deployment_revision"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Reconcile 使用 rendezvous hashing 选节点，同一 unit 每节点至多一个副本。
// 容量不足时在写 assignment 前失败，避免把半份计划推给 Node Agent。
func (s Scheduler) Reconcile(ctx context.Context, deployment deploymentv2.Deployment) (Plan, error) {
	if s.Nodes == nil || s.Assignments == nil {
		return Plan{}, errors.New("scheduler 的节点与 assignment KV 必须配置")
	}
	graph := make(map[string][]string, len(deployment.Units))
	for _, unit := range deployment.Units {
		graph[unit.ID] = append([]string(nil), unit.DependsOn...)
	}
	if s.Artifacts != nil {
		if err := validateDeploymentContracts(deployment, graph, s.Artifacts); err != nil {
			return Plan{}, err
		}
	}
	order, err := servicemodel.TopologicalOrder(graph)
	if err != nil {
		return Plan{}, fmt.Errorf("部署依赖图无效: %w", err)
	}
	unitsByID := make(map[string]deploymentv2.ServiceUnit, len(deployment.Units))
	for _, unit := range deployment.Units {
		unitsByID[unit.ID] = unit
	}
	nodes, err := s.liveNodes(ctx, deployment.Metadata.Tenant, deployment.Metadata.Name)
	if err != nil {
		return Plan{}, err
	}
	assignments := make(map[string]deploymentv1.DesiredState, len(nodes))
	for nodeID := range nodes {
		assignments[nodeID] = deploymentv1.DesiredState{
			Version:  1,
			Metadata: deploymentv1.Metadata{Name: deployment.Metadata.Name, Tenant: deployment.Metadata.Tenant},
			Units:    []deploymentv1.Unit{},
		}
	}
	available := make(map[string]controlplane.ResourceCapacity, len(nodes))
	occupied, err := s.occupiedResources(ctx, controlplane.AssignmentPrefix(deployment.Metadata.Tenant, deployment.Metadata.Name))
	if err != nil {
		return Plan{}, err
	}
	for nodeID, node := range nodes {
		capacity := node.Capacity
		capacity.CPUMillis -= occupied[nodeID].CPUMillis
		capacity.MemoryBytes -= occupied[nodeID].MemoryBytes
		capacity.GPU -= occupied[nodeID].GPU
		available[nodeID] = capacity
	}
	for _, unitID := range order {
		unit := unitsByID[unitID]
		if !unit.Enabled {
			continue
		}
		replicas, err := s.desiredReplicas(ctx, deployment, unit)
		if err != nil {
			return Plan{}, err
		}
		policy := servicemodel.Normalize(servicemodel.Policy{
			InstancePolicy: unit.InstancePolicy, StateModel: unit.StateModel,
			Visibility: unit.Visibility, Routing: unit.Routing, RoutingDomain: unit.RoutingDomain,
		})
		if err := servicemodel.Validate(policy); err != nil {
			return Plan{}, fmt.Errorf("unit %s 运行策略无效: %w", unit.ID, err)
		}
		eligible := eligibleNodes(nodes, available, unit)
		if len(eligible) < replicas {
			if policy.InstancePolicy == servicemodel.PolicyPartitioned && len(eligible) > 0 {
				replicas = len(eligible) // 节点故障时允许剩余 owner 接管全部分片并上报组合降级。
			} else {
				return Plan{}, fmt.Errorf("unit %s 需要 %d 副本，但只有 %d 个节点满足标签、亲和与资源约束", unit.ID, replicas, len(eligible))
			}
		}
		sort.Slice(eligible, func(i, j int) bool {
			leftPreference := preferenceScore(nodes[eligible[i]].Labels, unit.Placement)
			rightPreference := preferenceScore(nodes[eligible[j]].Labels, unit.Placement)
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
			partitionKeys := partitionAssignments[nodeID]
			if policy.InstancePolicy == servicemodel.PolicyPartitioned && len(partitionKeys) == 0 {
				continue
			}
			config := cloneConfig(unit.Config)
			if policy.InstancePolicy == servicemodel.PolicyPartitioned {
				if config == nil {
					config = map[string]any{}
				}
				config["partition_keys"] = append([]string(nil), partitionKeys...)
			}
			assignment := assignments[nodeID]
			assignment.Units = append(assignment.Units, deploymentv1.Unit{
				ID: unit.ID, Kind: unit.Kind, Plugins: append([]deploymentv1.PluginRef(nil), unit.Plugins...),
				Config: config, Enabled: true, ServiceRole: unit.ServiceRole,
				LogicalService: unit.LogicalService, InstancePolicy: policy.InstancePolicy, StateModel: policy.StateModel,
				Visibility: policy.Visibility, Routing: policy.Routing, RoutingDomain: policy.RoutingDomain, Replicas: 1,
				DependsOn: append([]string(nil), unit.DependsOn...),
				Resources: deploymentv1.ResourceRequirements{Requests: deploymentv1.ResourceList{
					CPUMillis: unit.Resources.Requests.CPUMillis, MemoryBytes: unit.Resources.Requests.MemoryBytes, GPU: unit.Resources.Requests.GPU,
				}},
			})
			assignments[nodeID] = assignment
			capacity := available[nodeID]
			capacity.CPUMillis -= unit.Resources.Requests.CPUMillis
			capacity.MemoryBytes -= unit.Resources.Requests.MemoryBytes
			capacity.GPU -= unit.Resources.Requests.GPU
			available[nodeID] = capacity
		}
	}
	for nodeID, assignment := range assignments {
		localUnits := make(map[string]struct{}, len(assignment.Units))
		for _, unit := range assignment.Units {
			localUnits[unit.ID] = struct{}{}
		}
		for i := range assignment.Units {
			localDependencies := assignment.Units[i].DependsOn[:0]
			for _, dependency := range assignment.Units[i].DependsOn {
				if _, local := localUnits[dependency]; local {
					localDependencies = append(localDependencies, dependency)
				}
			}
			assignment.Units[i].DependsOn = localDependencies
		}
		sort.Slice(assignment.Units, func(i, j int) bool { return assignment.Units[i].ID < assignment.Units[j].ID })
		assignments[nodeID] = assignment
	}

	prefix := controlplane.AssignmentPrefix(deployment.Metadata.Tenant, deployment.Metadata.Name)
	existing, maxGeneration, err := s.existingAssignments(ctx, deployment.Metadata.Tenant, deployment.Metadata.Name)
	if err != nil {
		return Plan{}, err
	}
	scheduleGeneration, err := s.scheduleGeneration(ctx, deployment.Metadata.Tenant, deployment.Metadata.Name)
	if err != nil {
		return Plan{}, err
	}
	if scheduleGeneration > maxGeneration {
		maxGeneration = scheduleGeneration
	}
	if assignmentsEqual(assignments, existing) {
		for nodeID, assignment := range assignments {
			assignment.Revision = maxGeneration
			assignments[nodeID] = assignment
		}
		return Plan{Generation: maxGeneration, Assignments: assignments}, nil
	}
	generation, err := s.reserveGeneration(ctx, deployment, maxGeneration)
	if err != nil {
		return Plan{}, err
	}
	for nodeID, assignment := range assignments {
		assignment.Revision = generation
		raw, err := json.Marshal(assignment)
		if err != nil {
			return Plan{}, err
		}
		key := controlplane.AssignmentKey(deployment.Metadata.Tenant, deployment.Metadata.Name, nodeID)
		if _, _, err := controlplane.ApplyDesiredState(ctx, s.Assignments, key, raw); err != nil {
			return Plan{}, fmt.Errorf("发布节点 %s assignment: %w", nodeID, err)
		}
		assignments[nodeID] = assignment
	}
	for key := range existing {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		nodeID := existing[key].NodeID
		if _, live := assignments[nodeID]; live {
			continue
		}
		if err := s.Assignments.Delete(ctx, key); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
			return Plan{}, fmt.Errorf("删除离线节点 assignment %s: %w", key, err)
		}
	}
	return Plan{Generation: generation, Assignments: assignments}, nil
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

func (s Scheduler) scheduleGeneration(ctx context.Context, tenant, name string) (uint64, error) {
	entry, err := s.Assignments.Get(ctx, controlplane.ScheduleKey(tenant, name))
	if errors.Is(err, jetstream.ErrKeyNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("读取调度 generation: %w", err)
	}
	var state scheduleState
	if err := json.Unmarshal(entry.Value(), &state); err != nil || state.SchemaVersion != 1 {
		return 0, errors.New("调度 generation 记录损坏")
	}
	return state.Generation, nil
}

func (s Scheduler) reserveGeneration(ctx context.Context, deployment deploymentv2.Deployment, floor uint64) (uint64, error) {
	key := controlplane.ScheduleKey(deployment.Metadata.Tenant, deployment.Metadata.Name)
	for range 8 {
		entry, err := s.Assignments.Get(ctx, key)
		generation := floor + 1
		state := scheduleState{
			SchemaVersion: 1, Generation: generation, DeploymentRevision: deployment.Revision, UpdatedAt: time.Now().UTC(),
		}
		if err == nil {
			var current scheduleState
			if json.Unmarshal(entry.Value(), &current) != nil || current.SchemaVersion != 1 {
				return 0, errors.New("调度 generation 记录损坏")
			}
			if current.Generation >= generation {
				state.Generation = current.Generation + 1
			}
		}
		raw, _ := json.Marshal(state)
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			if _, createErr := s.Assignments.Create(ctx, key, raw); createErr == nil {
				return state.Generation, nil
			}
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("读取调度 generation: %w", err)
		}
		if _, updateErr := s.Assignments.Update(ctx, key, raw, entry.Revision()); updateErr == nil {
			return state.Generation, nil
		}
	}
	return 0, errors.New("并发调度冲突过多，无法保留 generation")
}

type existingAssignment struct {
	NodeID string
	State  deploymentv1.DesiredState
}

func (s Scheduler) liveNodes(ctx context.Context, tenant, deployment string) (map[string]controlplane.NodeRecord, error) {
	keys, err := s.Nodes.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return map[string]controlplane.NodeRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("列出节点租约: %w", err)
	}
	nodes := make(map[string]controlplane.NodeRecord, len(keys))
	for _, key := range keys {
		entry, err := s.Nodes.Get(ctx, key)
		if err != nil {
			continue
		}
		var record controlplane.NodeRecord
		if err := json.Unmarshal(entry.Value(), &record); err != nil || record.ValidateBasic() != nil {
			return nil, fmt.Errorf("节点租约 %s 无效", key)
		}
		if key != controlplane.NodeKey(record.TenantID, record.Deployment, record.NodeID) {
			return nil, fmt.Errorf("节点租约 %s 与声明作用域不匹配", key)
		}
		if record.TenantID != tenant || record.Deployment != deployment {
			continue
		}
		nodes[record.NodeID] = record
	}
	return nodes, nil
}

func (s Scheduler) existingAssignments(ctx context.Context, tenant, name string) (map[string]existingAssignment, uint64, error) {
	prefix := controlplane.AssignmentPrefix(tenant, name)
	keys, err := s.Assignments.Keys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		return map[string]existingAssignment{}, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("列出既有 assignment: %w", err)
	}
	existing := map[string]existingAssignment{}
	var maxGeneration uint64
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		entry, err := s.Assignments.Get(ctx, key)
		if err != nil {
			continue
		}
		state, err := deploymentv1.Parse(entry.Value())
		if err != nil {
			return nil, 0, fmt.Errorf("既有 assignment %s 损坏: %w", key, err)
		}
		nodeID, err := controlplane.AssignmentNodeID(tenant, name, key)
		if err != nil {
			return nil, 0, err
		}
		existing[key] = existingAssignment{NodeID: nodeID, State: state}
		if state.Revision > maxGeneration {
			maxGeneration = state.Revision
		}
	}
	return existing, maxGeneration, nil
}

func eligibleNodes(nodes map[string]controlplane.NodeRecord, available map[string]controlplane.ResourceCapacity, unit deploymentv2.ServiceUnit) []string {
	var eligible []string
	for nodeID, node := range nodes {
		if matchesLabels(node.Labels, unit.Placement.NodeSelector) &&
			matchesRequiredAffinity(node.Labels, unit.Placement) &&
			fitsResources(available[nodeID], unit.Resources.Requests) {
			eligible = append(eligible, nodeID)
		}
	}
	return eligible
}

func matchesRequiredAffinity(labels map[string]string, placement deploymentv2.Placement) bool {
	for _, term := range placement.Affinity.Required {
		if !matchesLabels(labels, term.MatchLabels) {
			return false
		}
	}
	for _, term := range placement.AntiAffinity.Required {
		if matchesLabels(labels, term.MatchLabels) {
			return false
		}
	}
	return true
}

func preferenceScore(labels map[string]string, placement deploymentv2.Placement) int {
	score := 0
	for _, term := range placement.Affinity.Preferred {
		if matchesLabels(labels, term.MatchLabels) {
			score += term.Weight
		}
	}
	for _, term := range placement.AntiAffinity.Preferred {
		if matchesLabels(labels, term.MatchLabels) {
			score -= term.Weight
		}
	}
	return score
}

func matchesLabels(labels, selector map[string]string) bool {
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func fitsResources(capacity controlplane.ResourceCapacity, request deploymentv2.ResourceList) bool {
	return capacity.CPUMillis >= request.CPUMillis && capacity.MemoryBytes >= request.MemoryBytes && capacity.GPU >= request.GPU
}

func placementScore(unitID, nodeID string) uint64 {
	digest := sha256.Sum256([]byte(unitID + "\x00" + nodeID))
	return binary.BigEndian.Uint64(digest[:8])
}

// assignPartitions 为每个分片选择稳定 owner。先用 rendezvous 为每个候选节点保留
// 一个分片，再为剩余分片选择最高分节点，既避免空副本，也尽量减少节点变化时的迁移。
func assignPartitions(unit deploymentv2.ServiceUnit, nodes []string) map[string][]string {
	assigned := make(map[string][]string, len(nodes))
	if unit.InstancePolicy != servicemodel.PolicyPartitioned {
		for _, nodeID := range nodes {
			assigned[nodeID] = nil
		}
		return assigned
	}
	keys := append([]string(nil), unit.PartitionKeys...)
	sort.Strings(keys)
	remaining := append([]string(nil), keys...)
	for _, nodeID := range nodes {
		best := 0
		for index := 1; index < len(remaining); index++ {
			if placementScore(unit.ID+"\x00"+remaining[index], nodeID) > placementScore(unit.ID+"\x00"+remaining[best], nodeID) {
				best = index
			}
		}
		assigned[nodeID] = append(assigned[nodeID], remaining[best])
		remaining = append(remaining[:best], remaining[best+1:]...)
	}
	for _, key := range remaining {
		owner := nodes[0]
		for _, nodeID := range nodes[1:] {
			if placementScore(unit.ID+"\x00"+key, nodeID) > placementScore(unit.ID+"\x00"+key, owner) {
				owner = nodeID
			}
		}
		assigned[owner] = append(assigned[owner], key)
	}
	for nodeID := range assigned {
		sort.Strings(assigned[nodeID])
	}
	return assigned
}

func cloneConfig(config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	raw, _ := json.Marshal(config)
	var clone map[string]any
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func assignmentsEqual(planned map[string]deploymentv1.DesiredState, existing map[string]existingAssignment) bool {
	if len(planned) != len(existing) {
		return false
	}
	byNode := make(map[string]deploymentv1.DesiredState, len(existing))
	for _, item := range existing {
		byNode[item.NodeID] = item.State
	}
	for nodeID, state := range planned {
		old, ok := byNode[nodeID]
		if !ok {
			return false
		}
		state.Revision, old.Revision = 0, 0
		if state.Digest() != old.Digest() {
			return false
		}
	}
	return true
}

// Controller watch 全局部署和节点租约，任一变化都重新生成 assignment；周期轮询负责恢复 watcher 漏报。
type Controller struct {
	Deployments   jetstream.KeyValue
	Scheduler     Scheduler
	Leaders       jetstream.KeyValue
	DeploymentKey string
	Identity      string
	Interval      time.Duration
	Election      controlplane.LeaderElectionOptions
	Logf          func(string, ...any)
}

// convergenceSummary 是一次成功对账中值得写入 INFO 的稳定业务摘要。
// 节点续租和实际态 checkpoint 都会产生 KV 事件；相同摘要不应被误解为
// 一次新的调度状态变化。
type convergenceSummary struct {
	generation        uint64
	nodes             int
	compositionStatus string
	units             int
	hasComposition    bool
}

func shouldLogConvergence(last *convergenceSummary, current convergenceSummary) bool {
	return last == nil || *last != current
}

func (c Controller) Run(ctx context.Context) error {
	if c.Deployments == nil || c.DeploymentKey == "" || c.Leaders == nil || c.Identity == "" {
		return errors.New("controller deployment/leader KV、deployment key 与 identity 未配置")
	}
	if c.Scheduler.Nodes == nil || c.Scheduler.Assignments == nil {
		return errors.New("controller scheduler KV 未配置")
	}
	if c.Interval <= 0 {
		c.Interval = 5 * time.Second
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
	c.Election.Logf = c.Logf
	elector := controlplane.LeaderElector{
		KV: c.Leaders, Election: c.DeploymentKey, Identity: c.Identity,
		Options: c.Election,
	}
	for {
		leadership, err := elector.Acquire(ctx)
		if err != nil {
			return err
		}
		record := leadership.Record()
		c.Logf("controller 获得领导权 identity=%s election=%s token=%s", c.Identity, c.DeploymentKey, record.Token)
		leaderCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- c.runAsLeader(leaderCtx) }()
		select {
		case <-ctx.Done():
			cancel()
			<-done
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = leadership.Close(closeCtx)
			closeCancel()
			return ctx.Err()
		case lost := <-leadership.Lost():
			cancel()
			<-done
			c.Logf("controller 失去领导权 identity=%s: %v", c.Identity, lost)
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = leadership.Close(closeCtx)
			closeCancel()
			// 领导权丢失不是进程故障；回到 Acquire 等待当前 leader 退出或租约过期。
			continue
		case runErr := <-done:
			cancel()
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = leadership.Close(closeCtx)
			closeCancel()
			return runErr
		}
	}
}

func (c Controller) runAsLeader(ctx context.Context) error {
	deploymentWatcher, err := c.Deployments.Watch(ctx, c.DeploymentKey)
	if err != nil {
		return fmt.Errorf("watch 集群部署: %w", err)
	}
	defer func() {
		_ = deploymentWatcher.Stop() // context 结束后只做 watcher 本地回收，主错误更有诊断价值。
	}()
	nodeWatcher, err := c.Scheduler.Nodes.WatchAll(ctx)
	if err != nil {
		return fmt.Errorf("watch 节点租约: %w", err)
	}
	defer func() {
		_ = nodeWatcher.Stop() // 同上；停止失败不覆盖 controller 的退出原因。
	}()
	var actualWatcher jetstream.KeyWatcher
	if c.Scheduler.Actual != nil {
		actualWatcher, err = c.Scheduler.Actual.WatchAll(ctx)
		if err != nil {
			return fmt.Errorf("watch 节点实际态: %w", err)
		}
		defer func() { _ = actualWatcher.Stop() }()
	}
	var actualUpdates <-chan jetstream.KeyValueEntry
	if actualWatcher != nil {
		actualUpdates = actualWatcher.Updates()
	}
	var lastSummary *convergenceSummary
	reconcile := func(reason string) {
		entry, err := c.Deployments.Get(ctx, c.DeploymentKey)
		if err != nil {
			c.Logf("读取集群部署失败 reason=%s: %v", reason, err)
			return
		}
		deployment, err := deploymentv2.Parse(entry.Value())
		if err == nil {
			var plan Plan
			plan, err = c.Scheduler.Reconcile(ctx, deployment)
			if err == nil {
				summary := convergenceSummary{generation: plan.Generation, nodes: len(plan.Assignments)}
				if c.Scheduler.Actual != nil {
					if report, observeErr := c.Scheduler.ObserveComposition(ctx, deployment); observeErr == nil {
						summary.compositionStatus = string(report.Status)
						summary.units = len(report.Units)
						summary.hasComposition = true
					} else {
						c.Logf("组合状态观测失败 reason=%s: %v", reason, observeErr)
					}
				}
				if shouldLogConvergence(lastSummary, summary) {
					c.Logf("调度已收敛 reason=%s generation=%d nodes=%d", reason, plan.Generation, len(plan.Assignments))
					if summary.hasComposition {
						c.Logf("组合状态 reason=%s status=%s units=%d", reason, summary.compositionStatus, summary.units)
					}
					lastSummary = &summary
				}
			}
		}
		if err != nil {
			c.Logf("调度未收敛 reason=%s: %v", reason, err)
		}
	}
	reconcile("startup")
	ticker := time.NewTicker(c.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			reconcile("poll")
		case _, ok := <-deploymentWatcher.Updates():
			if !ok {
				return errors.New("集群部署 watcher 已关闭")
			}
			reconcile("deployment_watch")
		case _, ok := <-nodeWatcher.Updates():
			if !ok {
				return errors.New("节点 watcher 已关闭")
			}
			reconcile("node_watch")
		case _, ok := <-actualUpdates:
			if !ok {
				return errors.New("节点实际态 watcher 已关闭")
			}
			reconcile("actual_watch")
		}
	}
}
