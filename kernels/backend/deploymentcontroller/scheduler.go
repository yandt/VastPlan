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
	"sort"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
)

type Scheduler struct {
	Nodes       jetstream.KeyValue
	Assignments jetstream.KeyValue
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
	nodes, err := s.liveNodes(ctx)
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
	for _, unit := range deployment.Units {
		if !unit.Enabled {
			continue
		}
		eligible := eligibleNodes(nodes, unit.Placement.NodeSelector)
		if len(eligible) < unit.Replicas {
			return Plan{}, fmt.Errorf("unit %s 需要 %d 副本，但只有 %d 个节点匹配 selector", unit.ID, unit.Replicas, len(eligible))
		}
		sort.Slice(eligible, func(i, j int) bool {
			left, right := placementScore(unit.ID, eligible[i]), placementScore(unit.ID, eligible[j])
			if left != right {
				return left > right
			}
			return eligible[i] < eligible[j]
		})
		for _, nodeID := range eligible[:unit.Replicas] {
			assignment := assignments[nodeID]
			assignment.Units = append(assignment.Units, deploymentv1.Unit{
				ID: unit.ID, Kind: unit.Kind, Plugins: append([]deploymentv1.PluginRef(nil), unit.Plugins...),
				Config: cloneConfig(unit.Config), Enabled: true, ServiceRole: unit.ServiceRole, Replicas: 1,
			})
			assignments[nodeID] = assignment
		}
	}
	for nodeID, assignment := range assignments {
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

func (s Scheduler) liveNodes(ctx context.Context) (map[string]controlplane.NodeRecord, error) {
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
		if err := json.Unmarshal(entry.Value(), &record); err != nil || record.SchemaVersion != 1 || record.NodeID == "" {
			return nil, fmt.Errorf("节点租约 %s 无效", key)
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

func eligibleNodes(nodes map[string]controlplane.NodeRecord, selector map[string]string) []string {
	var eligible []string
	for nodeID, node := range nodes {
		matches := true
		for key, value := range selector {
			if node.Labels[key] != value {
				matches = false
				break
			}
		}
		if matches {
			eligible = append(eligible, nodeID)
		}
	}
	return eligible
}

func placementScore(unitID, nodeID string) uint64 {
	digest := sha256.Sum256([]byte(unitID + "\x00" + nodeID))
	return binary.BigEndian.Uint64(digest[:8])
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
				c.Logf("调度已收敛 reason=%s generation=%d nodes=%d", reason, plan.Generation, len(plan.Assignments))
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
		}
	}
}
