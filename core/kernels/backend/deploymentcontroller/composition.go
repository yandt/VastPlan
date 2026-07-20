package deploymentcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
)

type CompositionStatus = deploymentpublication.ReadinessStatus
type CompositionUnit = deploymentpublication.ReadinessUnit
type CompositionReport = deploymentpublication.ReadinessObservation

const (
	CompositionPending        = deploymentpublication.ReadinessPending
	CompositionBlocked        = deploymentpublication.ReadinessBlocked
	CompositionReady          = deploymentpublication.ReadinessReady
	CompositionDegraded       = deploymentpublication.ReadinessDegraded
	CompositionDependencyLost = deploymentpublication.ReadinessDependencyLost
	CompositionFailed         = deploymentpublication.ReadinessFailed
	CompositionStopped        = deploymentpublication.ReadinessStopped
)

type actualSnapshot struct {
	AppliedRevision uint64                `json:"applied_revision"`
	Units           map[string]actualUnit `json:"units"`
}

type actualUnit struct {
	AppliedRevision  uint64           `json:"applied_revision"`
	Phase            string           `json:"phase"`
	Readiness        string           `json:"readiness"`
	DependencyIssues []string         `json:"dependency_issues,omitempty"`
	LastError        string           `json:"last_error,omitempty"`
	Candidate        *actualCandidate `json:"candidate,omitempty"`
}

type actualCandidate struct {
	Phase     string `json:"phase"`
	LastError string `json:"last_error,omitempty"`
}

// ObserveComposition 汇总各 Node Agent 上报的实际态，不把“存在一个进程”误报为 Ready。
func (s Scheduler) ObserveComposition(ctx context.Context, deployment deploymentv2.Deployment) (CompositionReport, error) {
	if s.Actual == nil || s.Assignments == nil {
		return CompositionReport{}, errors.New("scheduler 未配置 actual/assignment KV")
	}
	assignmentKeys, err := s.Assignments.Keys(ctx)
	if err != nil && !errors.Is(err, jetstream.ErrNoKeysFound) {
		return CompositionReport{}, fmt.Errorf("读取节点 assignment key: %w", err)
	}
	byUnit := make(map[string][]actualUnit)
	desiredReplicas := map[string]int{}
	for _, unit := range deployment.Units {
		if !unit.Enabled {
			continue
		}
		replicas, replicaErr := s.desiredReplicas(ctx, deployment, unit)
		if replicaErr != nil {
			return CompositionReport{}, replicaErr
		}
		desiredReplicas[unit.ID] = replicas
	}
	var generation uint64
	prefix := controlplane.AssignmentPrefix(deployment.Metadata.Tenant, deployment.Metadata.Name)
	for _, assignmentKey := range assignmentKeys {
		if len(assignmentKey) < len(prefix) || assignmentKey[:len(prefix)] != prefix {
			continue
		}
		assignmentEntry, getErr := s.Assignments.Get(ctx, assignmentKey)
		if getErr != nil {
			continue
		}
		assignment, parseErr := deploymentv1.Parse(assignmentEntry.Value())
		if parseErr != nil {
			return CompositionReport{}, fmt.Errorf("解析组合 assignment %s: %w", assignmentKey, parseErr)
		}
		if assignment.Revision > generation {
			generation = assignment.Revision
		}
		nodeID, nodeErr := controlplane.AssignmentNodeID(deployment.Metadata.Tenant, deployment.Metadata.Name, assignmentKey)
		if nodeErr != nil {
			return CompositionReport{}, nodeErr
		}
		entry, getErr := s.Actual.Get(ctx, controlplane.ActualKey(deployment.Metadata.Tenant, deployment.Metadata.Name, nodeID))
		if getErr != nil {
			continue
		}
		var actual actualSnapshot
		if json.Unmarshal(entry.Value(), &actual) != nil {
			continue
		}
		for _, desired := range assignment.Units {
			state, exists := actual.Units[desired.ID]
			if !exists {
				continue
			}
			if state.Candidate != nil && state.Candidate.Phase == "failed" {
				failed := state
				failed.Phase, failed.LastError = "failed", state.Candidate.LastError
				byUnit[desired.ID] = append(byUnit[desired.ID], failed)
				continue
			}
			if actual.AppliedRevision != assignment.Revision || state.AppliedRevision != assignment.Revision {
				continue
			}
			byUnit[desired.ID] = append(byUnit[desired.ID], state)
		}
	}
	report := CompositionReport{
		SchemaVersion: 1, Tenant: deployment.Metadata.Tenant, Deployment: deployment.Metadata.Name,
		Revision: deployment.Revision, Generation: generation, Status: CompositionReady, UpdatedAt: time.Now().UTC(),
	}
	index := map[string]int{}
	for _, unit := range deployment.Units {
		if !unit.Enabled {
			continue
		}
		observed := byUnit[unit.ID]
		item := CompositionUnit{ID: unit.ID, DesiredReplicas: desiredReplicas[unit.ID], Replicas: len(observed)}
		for _, state := range observed {
			switch {
			case state.Phase == "failed":
				item.Errors = append(item.Errors, state.LastError)
			case state.Phase == "active" && state.Readiness == "degraded":
				item.DependencyIssues = append(item.DependencyIssues, state.DependencyIssues...)
			case state.Phase == "active":
				item.ReadyReplicas++
			}
		}
		if item.Replicas == 0 {
			item.Status = CompositionPending
		} else if len(item.Errors) > 0 {
			item.Status = CompositionFailed
		} else if len(item.DependencyIssues) > 0 {
			item.Status = CompositionDependencyLost
		} else if item.ReadyReplicas < item.DesiredReplicas {
			item.Status = CompositionDegraded
		} else {
			item.Status = CompositionReady
		}
		index[unit.ID] = len(report.Units)
		report.Units = append(report.Units, item)
		if statusRank(item.Status) > statusRank(report.Status) {
			report.Status = item.Status
		}
	}
	for _, unit := range deployment.Units {
		itemIndex, exists := index[unit.ID]
		if !exists || report.Units[itemIndex].Status == CompositionFailed {
			continue
		}
		for _, dependency := range unit.DependsOn {
			dependencyIndex, dependencyExists := index[dependency]
			if (!dependencyExists || report.Units[dependencyIndex].Status != CompositionReady) &&
				statusRank(report.Units[itemIndex].Status) < statusRank(CompositionDependencyLost) {
				report.Units[itemIndex].Status = CompositionBlocked
				report.Units[itemIndex].DependencyIssues = append(report.Units[itemIndex].DependencyIssues, "等待依赖 "+dependency)
			}
		}
	}
	report.Status = CompositionReady
	for _, item := range report.Units {
		if statusRank(item.Status) > statusRank(report.Status) {
			report.Status = item.Status
		}
	}
	sort.Slice(report.Units, func(i, j int) bool { return report.Units[i].ID < report.Units[j].ID })
	if s.Compositions != nil {
		raw, marshalErr := json.Marshal(report)
		if marshalErr != nil {
			return CompositionReport{}, marshalErr
		}
		if _, putErr := s.Compositions.Put(ctx, controlplane.CompositionKey(deployment.Metadata.Tenant, deployment.Metadata.Name), raw); putErr != nil {
			return CompositionReport{}, fmt.Errorf("持久化组合状态: %w", putErr)
		}
	}
	return report, nil
}

func statusRank(status CompositionStatus) int {
	switch status {
	case CompositionFailed:
		return 6
	case CompositionDependencyLost:
		return 5
	case CompositionBlocked:
		return 4
	case CompositionDegraded:
		return 3
	case CompositionPending:
		return 2
	case CompositionStopped:
		return 1
	default:
		return 0
	}
}
