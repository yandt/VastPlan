package deploymentcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"github.com/nats-io/nats.go/jetstream"
)

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
	// Assignment generations and Deployment revisions are independent counters,
	// but a newly rebuilt control plane must not publish a lower generation than
	// the durable Deployment revision. This also preserves Node Agent's strict
	// same-revision/different-content conflict protection after control-plane
	// disaster recovery.
	if deployment.Revision > 0 && deployment.Revision-1 > floor {
		floor = deployment.Revision - 1
	}
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
