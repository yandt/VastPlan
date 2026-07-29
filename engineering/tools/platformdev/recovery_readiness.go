package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
)

type recoveryStatus = recoveryv1.Status

func (r *runtime) startRecoveryMonitor(ctx context.Context, startedAt time.Time) error {
	capsule, err := loadRecoveryCapsule(filepath.Join(r.runDir, recoveryCapsuleFilename))
	if err != nil {
		return err
	}
	r.updateRecoveryStatus(evaluateRecoveryCapsule(capsule, readinessState{}, startedAt))
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		previous := ""
		for {
			status := r.observeRecoveryCapsule(capsule, startedAt)
			r.updateRecoveryStatus(status)
			fingerprint := recoveryStatusFingerprint(status)
			if fingerprint != previous {
				log.Printf("Seed Recovery Capsule 状态: %s", fingerprint)
				previous = fingerprint
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

func (r *runtime) waitForRecoveryStage(ctx context.Context, stageID string, startedAt time.Time, timeout time.Duration) error {
	capsule, err := loadRecoveryCapsule(filepath.Join(r.runDir, recoveryCapsuleFilename))
	if err != nil {
		return err
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	last := "尚未观察"
	for {
		status := r.observeRecoveryCapsule(capsule, startedAt)
		r.updateRecoveryStatus(status)
		for _, stage := range status.Stages {
			if stage.ID != stageID {
				continue
			}
			last = fmt.Sprintf("status=%s ready=%d/%d issues=%s", stage.Status, stage.Ready, stage.Required, strings.Join(stage.Issues, "; "))
			if stage.Status == "Ready" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("等待 Recovery Capsule 阶段 %s 超时: %s", stageID, last)
		case <-ticker.C:
		}
	}
}

func (r *runtime) observeRecoveryCapsule(capsule recoveryv1.Capsule, startedAt time.Time) recoveryStatus {
	raw, err := os.ReadFile(filepath.Join(r.persistentStateRoot(), "actual-state.json"))
	if err != nil {
		return evaluateRecoveryCapsule(capsule, readinessState{}, startedAt)
	}
	var state readinessState
	if err := json.Unmarshal(raw, &state); err != nil {
		state.Errors = []readinessError{{Stage: "decode", Message: "invalid"}}
	}
	return evaluateRecoveryCapsule(capsule, state, startedAt)
}

func evaluateRecoveryCapsule(capsule recoveryv1.Capsule, state readinessState, startedAt time.Time) recoveryStatus {
	units := make(map[string]recoveryv1.UnitObservation, len(state.Units))
	for id, unit := range state.Units {
		units[id] = recoveryv1.UnitObservation{Phase: unit.Phase, Readiness: unit.Readiness, Candidate: unit.Candidate != nil}
	}
	now := time.Now().UTC()
	report := recoveryv1.BuildNodeReport(capsule, recoveryv1.RuntimeObservation{
		NodeID: "local-platform-node", ObservedRevision: state.ObservedRevision, AppliedRevision: state.AppliedRevision,
		UpdatedAt: state.UpdatedAt, Units: units, ReconcileFailed: len(state.Errors) > 0,
	}, recoveryv1.EvaluationPolicy{Now: now, NotBefore: startedAt})
	return recoveryv1.Aggregate(capsule, []recoveryv1.NodeReport{report}, now, 0)
}

func loadRecoveryCapsule(filename string) (recoveryv1.Capsule, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return recoveryv1.Capsule{}, err
	}
	return recoveryv1.ParseCapsule(raw)
}

func (r *runtime) updateRecoveryStatus(status recoveryStatus) {
	r.mu.Lock()
	r.recovery = status
	r.mu.Unlock()
}

func recoveryStatusFingerprint(status recoveryStatus) string {
	parts := []string{status.Overall}
	for _, stage := range status.Stages {
		parts = append(parts, fmt.Sprintf("%s=%s(%d/%d)", stage.ID, stage.Status, stage.Ready, stage.Required))
	}
	return strings.Join(parts, " ")
}
