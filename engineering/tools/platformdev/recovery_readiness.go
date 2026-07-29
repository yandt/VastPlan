package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
)

type recoveryStatus struct {
	CapsuleID     string                `json:"capsuleId"`
	RepositoryID  string                `json:"repositoryId"`
	Generation    uint64                `json:"generation"`
	ArtifactCount int                   `json:"artifactCount"`
	Overall       string                `json:"overall"`
	Stages        []recoveryStageStatus `json:"stages"`
	UpdatedAt     time.Time             `json:"updatedAt"`
}

type recoveryStageStatus struct {
	ID       string   `json:"id"`
	Status   string   `json:"status"`
	Ready    int      `json:"ready"`
	Required int      `json:"required"`
	Issues   []string `json:"issues,omitempty"`
}

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
		return recoveryStatus{CapsuleID: capsule.ID, RepositoryID: capsule.Inventory.RepositoryID, Generation: capsule.Inventory.Generation, ArtifactCount: len(capsule.Artifacts), Overall: "Failed", UpdatedAt: time.Now().UTC(), Stages: []recoveryStageStatus{{ID: recoveryv1.StageRecovery, Status: "Failed", Issues: []string{"actual-state 无法解析"}}}}
	}
	return evaluateRecoveryCapsule(capsule, state, startedAt)
}

func evaluateRecoveryCapsule(capsule recoveryv1.Capsule, state readinessState, startedAt time.Time) recoveryStatus {
	status := recoveryStatus{
		CapsuleID: capsule.ID, RepositoryID: capsule.Inventory.RepositoryID,
		Generation: capsule.Inventory.Generation, ArtifactCount: len(capsule.Artifacts),
		Overall: "Starting", UpdatedAt: time.Now().UTC(),
	}
	globalIssue := ""
	if state.UpdatedAt.IsZero() || state.UpdatedAt.Before(startedAt) {
		globalIssue = "actual-state 尚未由本次 Node Agent 更新"
	} else if state.ObservedRevision == 0 || state.AppliedRevision != state.ObservedRevision {
		globalIssue = fmt.Sprintf("revision 尚未提交 observed=%d applied=%d", state.ObservedRevision, state.AppliedRevision)
	} else if len(state.Errors) > 0 {
		last := state.Errors[len(state.Errors)-1]
		globalIssue = "reconcile 失败 stage=" + last.Stage + " message=" + last.Message
	}
	highestReady := ""
	for _, stage := range capsule.Stages {
		required, _ := recoveryv1.RequiredUnits(capsule.Stages, stage.ID)
		stageStatus := recoveryStageStatus{ID: stage.ID, Status: "Pending", Required: len(required)}
		if globalIssue != "" {
			stageStatus.Issues = []string{globalIssue}
			status.Stages = append(status.Stages, stageStatus)
			continue
		}
		failed, degraded := false, false
		for _, unitID := range required {
			unit, exists := state.Units[unitID]
			switch {
			case !exists:
				stageStatus.Issues = append(stageStatus.Issues, unitID+"=missing")
			case unit.Candidate != nil:
				stageStatus.Issues = append(stageStatus.Issues, unitID+"=candidate-pending")
			case unit.Phase == "active" && (unit.Readiness == "ready" || unit.Readiness == ""):
				stageStatus.Ready++
			case unit.Phase == "active" && unit.Readiness == "degraded":
				degraded = true
				stageStatus.Issues = append(stageStatus.Issues, unitID+"=degraded")
			case unit.Phase == "failed" || unit.Phase == "stopped":
				failed = true
				stageStatus.Issues = append(stageStatus.Issues, unitID+"="+unit.Phase)
			default:
				issue := unitID + "=" + unit.Phase + "/" + unit.Readiness
				if unit.LastError != "" {
					issue += ": " + unit.LastError
				}
				stageStatus.Issues = append(stageStatus.Issues, issue)
			}
		}
		sort.Strings(stageStatus.Issues)
		switch {
		case failed:
			stageStatus.Status = "Failed"
		case degraded:
			stageStatus.Status = "Degraded"
		case stageStatus.Ready == stageStatus.Required:
			stageStatus.Status = "Ready"
			highestReady = stage.ID
		}
		status.Stages = append(status.Stages, stageStatus)
	}
	switch highestReady {
	case recoveryv1.StagePlatform:
		status.Overall = "PlatformReady"
	case recoveryv1.StageControlPlane:
		status.Overall = "ControlPlaneReady"
	case recoveryv1.StageRecovery:
		status.Overall = "RecoveryReady"
	default:
		if len(status.Stages) > 0 && (status.Stages[0].Status == "Failed" || status.Stages[0].Status == "Degraded") {
			status.Overall = status.Stages[0].Status
		}
	}
	return status
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
