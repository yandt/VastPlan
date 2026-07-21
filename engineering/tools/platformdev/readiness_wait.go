package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

type readinessState struct {
	ObservedRevision uint64                   `json:"observed_revision"`
	AppliedRevision  uint64                   `json:"applied_revision"`
	Errors           []readinessError         `json:"errors"`
	UpdatedAt        time.Time                `json:"updated_at"`
	Units            map[string]readinessUnit `json:"units"`
}

type readinessError struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
}

type readinessUnit struct {
	Phase     string    `json:"phase"`
	Readiness string    `json:"readiness"`
	LastError string    `json:"last_error"`
	Candidate *struct{} `json:"candidate"`
}

func waitForUnits(ctx context.Context, filename string, count int, startedAt time.Time, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var last string
	for {
		if raw, err := os.ReadFile(filename); err == nil {
			var state readinessState
			if json.Unmarshal(raw, &state) == nil {
				ready, summary := unitsReadyAfterStart(state, count, startedAt)
				last = summary
				if ready {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("等待 %d 个 active unit 超时: %s", count, last)
		case <-ticker.C:
		}
	}
}

func unitsReadyAfterStart(state readinessState, count int, startedAt time.Time) (bool, string) {
	if state.UpdatedAt.IsZero() || state.UpdatedAt.Before(startedAt) {
		return false, "实际态尚未由本次 Node Agent 更新"
	}
	if state.ObservedRevision == 0 || state.AppliedRevision != state.ObservedRevision {
		return false, fmt.Sprintf("revision 尚未提交 observed=%d applied=%d", state.ObservedRevision, state.AppliedRevision)
	}
	if len(state.Errors) != 0 {
		last := state.Errors[len(state.Errors)-1]
		return false, "reconcile 失败 stage=" + last.Stage + " message=" + last.Message
	}
	active := 0
	messages := make([]string, 0, len(state.Units))
	for id, unit := range state.Units {
		status := id + "=" + unit.Phase + "/" + unit.Readiness
		if unit.Candidate != nil {
			status += " candidate=pending"
		}
		if unit.LastError != "" {
			status += " " + unit.LastError
		}
		messages = append(messages, status)
		if unit.Candidate == nil && unit.Phase == "active" && (unit.Readiness == "ready" || unit.Readiness == "") {
			active++
		}
	}
	sort.Strings(messages)
	return len(state.Units) == count && active == count, strings.Join(messages, "; ")
}
