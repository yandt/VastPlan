package repositoryruntime

import (
	"sort"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

const maxAssessmentInventoryRevisions = 128

type AssessmentInventory struct {
	ObservedAt         time.Time                  `json:"observedAt"`
	ReportArchiveReady bool                       `json:"reportArchiveReady"`
	Truncated          bool                       `json:"truncated"`
	Revisions          []AssessmentRevisionStatus `json:"revisions"`
}

type AssessmentRevisionStatus struct {
	DatabaseRevision string    `json:"databaseRevision"`
	Artifacts        int       `json:"artifacts"`
	Current          int       `json:"current"`
	Failed           int       `json:"failed"`
	Stale            int       `json:"stale"`
	Invalid          int       `json:"invalid"`
	LastEvaluatedAt  time.Time `json:"lastEvaluatedAt,omitempty"`
}

// AssessmentInventory exposes only evidence revisions already accepted by the
// repository. It does not claim that every node-local materializer is healthy.
func (m *Manager) AssessmentInventory(now time.Time) AssessmentInventory {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result := AssessmentInventory{ObservedAt: now.UTC(), ReportArchiveReady: m != nil && m.assessmentReports != nil && m.assessmentReports.Ready() == nil, Revisions: []AssessmentRevisionStatus{}}
	if m == nil {
		return result
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return result
	}
	_, entries := m.active.catalog.Entries()
	groups := map[string]*AssessmentRevisionStatus{}
	for _, entry := range entries {
		if entry.SecurityAdmission == nil {
			continue
		}
		revision := entry.SecurityAdmission.DatabaseRevision
		status := groupAssessmentRevision(groups, revision)
		status.Artifacts++
		chainRaw, err := m.active.signed.ReadSecurityStatusChain(entry.Ref)
		if err != nil {
			status.Invalid++
			continue
		}
		records, err := artifactassessment.InspectStatusChain(chainRaw)
		if err != nil {
			status.Invalid++
			continue
		}
		if len(records) == 0 {
			evaluatedAt, evaluatedErr := time.Parse(time.RFC3339Nano, entry.SecurityAdmission.EvaluatedAt)
			expiresAt, expiresErr := time.Parse(time.RFC3339Nano, entry.SecurityAdmission.ExpiresAt)
			if evaluatedErr != nil || expiresErr != nil {
				status.Invalid++
				continue
			}
			status.LastEvaluatedAt = laterTime(status.LastEvaluatedAt, evaluatedAt)
			if !expiresAt.After(now) {
				status.Stale++
			} else if entry.SecurityAdmission.Decision == artifactassessment.DecisionPass {
				status.Current++
			} else {
				status.Failed++
			}
			continue
		}
		latest, _, err := artifactassessment.InspectStatus(records[len(records)-1])
		if err != nil {
			status.Invalid++
			continue
		}
		if latest.Evaluation.Scanner.DatabaseRevision != revision {
			status.Artifacts--
			status = groupAssessmentRevision(groups, latest.Evaluation.Scanner.DatabaseRevision)
			status.Artifacts++
		}
		status.LastEvaluatedAt = laterTime(status.LastEvaluatedAt, latest.Evaluation.EvaluatedAt)
		if !latest.Evaluation.ExpiresAt.After(now) {
			status.Stale++
		} else if latest.Evaluation.Decision == artifactassessment.DecisionPass {
			status.Current++
		} else {
			status.Failed++
		}
	}
	for _, status := range groups {
		if status.Artifacts > 0 {
			result.Revisions = append(result.Revisions, *status)
		}
	}
	sort.Slice(result.Revisions, func(i, j int) bool {
		if !result.Revisions[i].LastEvaluatedAt.Equal(result.Revisions[j].LastEvaluatedAt) {
			return result.Revisions[i].LastEvaluatedAt.After(result.Revisions[j].LastEvaluatedAt)
		}
		return result.Revisions[i].DatabaseRevision < result.Revisions[j].DatabaseRevision
	})
	if len(result.Revisions) > maxAssessmentInventoryRevisions {
		result.Truncated = true
		result.Revisions = result.Revisions[:maxAssessmentInventoryRevisions]
	}
	return result
}

func groupAssessmentRevision(groups map[string]*AssessmentRevisionStatus, revision string) *AssessmentRevisionStatus {
	status := groups[revision]
	if status == nil {
		status = &AssessmentRevisionStatus{DatabaseRevision: revision}
		groups[revision] = status
	}
	return status
}

func laterTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}
