package recoveryv1

import (
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	UnitPending  = "Pending"
	UnitReady    = "Ready"
	UnitDegraded = "Degraded"
	UnitFailed   = "Failed"

	OverallStarting          = "Starting"
	OverallRecoveryReady     = "RecoveryReady"
	OverallControlPlaneReady = "ControlPlaneReady"
	OverallPlatformReady     = "PlatformReady"
)

// RuntimeObservation is the transport-neutral projection of one Node Agent's
// actual state. It contains no plugin error text, paths, credentials or PIDs.
type RuntimeObservation struct {
	NodeID           string
	ObservedRevision uint64
	AppliedRevision  uint64
	UpdatedAt        time.Time
	Units            map[string]UnitObservation
	ReconcileFailed  bool
}

type UnitObservation struct {
	Phase     string
	Readiness string
	Candidate bool
}

type EvaluationPolicy struct {
	Now       time.Time
	NotBefore time.Time
	MaxAge    time.Duration
}

// NodeReport is safe to sign into a Node Lease. It intentionally retains only
// bounded lifecycle categories for Capsule units.
type NodeReport struct {
	SchemaVersion    int                   `json:"schemaVersion"`
	CapsuleID        string                `json:"capsuleId"`
	RepositoryID     string                `json:"repositoryId"`
	Generation       uint64                `json:"generation"`
	NodeID           string                `json:"nodeId"`
	ObservedRevision uint64                `json:"observedRevision"`
	AppliedRevision  uint64                `json:"appliedRevision"`
	Units            map[string]UnitReport `json:"units"`
	UpdatedAt        time.Time             `json:"updatedAt"`
}

type UnitReport struct {
	Status string `json:"status"`
}

func ValidateNodeReport(report NodeReport) error {
	if report.SchemaVersion != Version || strings.TrimSpace(report.CapsuleID) == "" || strings.TrimSpace(report.RepositoryID) == "" || report.Generation == 0 || strings.TrimSpace(report.NodeID) == "" || report.UpdatedAt.IsZero() || len(report.Units) == 0 || len(report.Units) > 1024 {
		return errors.New("Recovery Node Report 身份或大小无效")
	}
	for id, unit := range report.Units {
		if strings.TrimSpace(id) == "" || unit.Status != UnitPending && unit.Status != UnitReady && unit.Status != UnitDegraded && unit.Status != UnitFailed {
			return errors.New("Recovery Node Report unit 无效")
		}
	}
	return nil
}

func CloneNodeReport(report NodeReport) NodeReport {
	clone := report
	clone.Units = make(map[string]UnitReport, len(report.Units))
	for id, unit := range report.Units {
		clone.Units[id] = unit
	}
	return clone
}

type Status struct {
	SchemaVersion    int           `json:"schemaVersion"`
	CapsuleID        string        `json:"capsuleId"`
	RepositoryID     string        `json:"repositoryId"`
	Generation       uint64        `json:"generation"`
	ArtifactCount    int           `json:"artifactCount"`
	Overall          string        `json:"overall"`
	Scope            string        `json:"scope"`
	ClusterAvailable bool          `json:"clusterAvailable"`
	Nodes            int           `json:"nodes"`
	Stages           []StageStatus `json:"stages"`
	UpdatedAt        time.Time     `json:"updatedAt"`
}

type StageStatus struct {
	ID       string   `json:"id"`
	Status   string   `json:"status"`
	Ready    int      `json:"ready"`
	Required int      `json:"required"`
	Issues   []string `json:"issues,omitempty"`
}

func BuildNodeReport(capsule Capsule, observation RuntimeObservation, policy EvaluationPolicy) NodeReport {
	now := policy.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	report := NodeReport{
		SchemaVersion: Version, CapsuleID: capsule.ID, RepositoryID: capsule.Inventory.RepositoryID,
		Generation: capsule.Inventory.Generation, NodeID: observation.NodeID,
		ObservedRevision: observation.ObservedRevision, AppliedRevision: observation.AppliedRevision,
		Units: map[string]UnitReport{}, UpdatedAt: now,
	}
	validState := !observation.UpdatedAt.IsZero() && !observation.UpdatedAt.Before(policy.NotBefore) &&
		(policy.MaxAge <= 0 || !observation.UpdatedAt.Before(now.Add(-policy.MaxAge))) &&
		observation.ObservedRevision > 0 && observation.AppliedRevision == observation.ObservedRevision && !observation.ReconcileFailed
	for _, stage := range capsule.Stages {
		for _, requirement := range stage.Units {
			status := UnitPending
			unit, exists := observation.Units[requirement.ID]
			if validState && exists {
				switch {
				case unit.Candidate:
					status = UnitPending
				case unit.Phase == "active" && (unit.Readiness == "ready" || unit.Readiness == ""):
					status = UnitReady
				case unit.Phase == "active" && unit.Readiness == "degraded":
					status = UnitDegraded
				case unit.Phase == "failed" || unit.Phase == "stopped":
					status = UnitFailed
				}
			}
			report.Units[requirement.ID] = UnitReport{Status: status}
		}
	}
	return report
}

// Aggregate accepts only fresh reports bound to this exact Capsule identity.
// Quorum is evaluated per unit and stages are cumulative.
func Aggregate(capsule Capsule, reports []NodeReport, now time.Time, maxAge time.Duration) Status {
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	accepted := make([]NodeReport, 0, len(reports))
	for _, report := range reports {
		if report.SchemaVersion != Version || report.CapsuleID != capsule.ID || report.RepositoryID != capsule.Inventory.RepositoryID || report.Generation != capsule.Inventory.Generation || report.NodeID == "" || report.UpdatedAt.IsZero() {
			continue
		}
		if maxAge > 0 && report.UpdatedAt.Before(now.Add(-maxAge)) {
			continue
		}
		accepted = append(accepted, report)
	}
	status := Status{
		SchemaVersion: Version, CapsuleID: capsule.ID, RepositoryID: capsule.Inventory.RepositoryID,
		Generation: capsule.Inventory.Generation, ArtifactCount: len(capsule.Artifacts),
		Overall: OverallStarting, Scope: "local", Nodes: len(accepted), UpdatedAt: now,
	}
	highestReady := ""
	for _, stage := range capsule.Stages {
		requirements, _ := RequiredUnitRequirements(capsule.Stages, stage.ID)
		stageStatus := StageStatus{ID: stage.ID, Status: UnitPending}
		failed, degraded := false, false
		for _, requirement := range requirements {
			ready, unhealthy := 0, 0
			for _, report := range accepted {
				switch report.Units[requirement.ID].Status {
				case UnitReady:
					ready++
				case UnitFailed:
					unhealthy++
				case UnitDegraded:
					degraded = true
				}
			}
			required := int(requirement.MinReady)
			stageStatus.Required += required
			if ready > required {
				ready = required
			}
			stageStatus.Ready += ready
			if ready < required {
				stageStatus.Issues = append(stageStatus.Issues, requirement.ID)
				if ready == 0 && unhealthy > 0 {
					failed = true
				} else if ready > 0 {
					degraded = true
				}
			}
		}
		sort.Strings(stageStatus.Issues)
		switch {
		case stageStatus.Ready == stageStatus.Required:
			stageStatus.Status = UnitReady
			highestReady = stage.ID
		case failed:
			stageStatus.Status = UnitFailed
		case degraded:
			stageStatus.Status = UnitDegraded
		}
		status.Stages = append(status.Stages, stageStatus)
	}
	switch highestReady {
	case StagePlatform:
		status.Overall = OverallPlatformReady
	case StageControlPlane:
		status.Overall = OverallControlPlaneReady
	case StageRecovery:
		status.Overall = OverallRecoveryReady
	}
	return status
}
