package deploymentcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/nats-io/nats.go/jetstream"

	deploymentv2 "cdsoft.com.cn/VastPlan/schemas/deployment/v2"
)

type CompositionStatus string

const (
	CompositionPending        CompositionStatus = "Pending"
	CompositionReady          CompositionStatus = "Ready"
	CompositionDegraded       CompositionStatus = "Degraded"
	CompositionDependencyLost CompositionStatus = "DependencyLost"
	CompositionFailed         CompositionStatus = "Failed"
	CompositionStopped        CompositionStatus = "Stopped"
)

type CompositionUnit struct {
	ID               string
	Status           CompositionStatus
	Replicas         int
	ReadyReplicas    int
	DependencyIssues []string
	Errors           []string
}

type CompositionReport struct {
	Deployment string
	Units      []CompositionUnit
	Status     CompositionStatus
}

type actualSnapshot struct {
	Units map[string]actualUnit `json:"units"`
}

type actualUnit struct {
	Phase            string   `json:"phase"`
	Readiness        string   `json:"readiness"`
	DependencyIssues []string `json:"dependency_issues,omitempty"`
	LastError        string   `json:"last_error,omitempty"`
}

// ObserveComposition 汇总各 Node Agent 上报的实际态，不把“存在一个进程”误报为 Ready。
func (s Scheduler) ObserveComposition(ctx context.Context, deployment deploymentv2.Deployment) (CompositionReport, error) {
	if s.Actual == nil {
		return CompositionReport{}, errors.New("scheduler 未配置 actual KV")
	}
	keys, err := s.Actual.Keys(ctx)
	if err != nil && !errors.Is(err, jetstream.ErrNoKeysFound) {
		return CompositionReport{}, fmt.Errorf("读取节点实际态 key: %w", err)
	}
	byUnit := make(map[string][]actualUnit)
	for _, key := range keys {
		entry, getErr := s.Actual.Get(ctx, key)
		if getErr != nil {
			continue
		}
		var actual actualSnapshot
		if json.Unmarshal(entry.Value(), &actual) != nil {
			continue
		}
		for id, state := range actual.Units {
			byUnit[id] = append(byUnit[id], state)
		}
	}
	report := CompositionReport{Deployment: deployment.Metadata.Name, Status: CompositionReady}
	for _, unit := range deployment.Units {
		if !unit.Enabled {
			continue
		}
		observed := byUnit[unit.ID]
		item := CompositionUnit{ID: unit.ID, Replicas: len(observed)}
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
		} else if item.ReadyReplicas < unit.Replicas {
			item.Status = CompositionDegraded
		} else {
			item.Status = CompositionReady
		}
		report.Units = append(report.Units, item)
		if statusRank(item.Status) > statusRank(report.Status) {
			report.Status = item.Status
		}
	}
	sort.Slice(report.Units, func(i, j int) bool { return report.Units[i].ID < report.Units[j].ID })
	return report, nil
}

func statusRank(status CompositionStatus) int {
	switch status {
	case CompositionFailed:
		return 5
	case CompositionDependencyLost:
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
