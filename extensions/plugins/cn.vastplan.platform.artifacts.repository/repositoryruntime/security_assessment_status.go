package repositoryruntime

import (
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

// SecurityAssessmentStats is deliberately low-cardinality. Detailed artifact
// identities remain in governed Catalog queries and never become metric labels.
type SecurityAssessmentStats struct {
	Artifacts        int  `json:"artifacts"`
	Unassessed       int  `json:"unassessed"`
	AdmissionCurrent int  `json:"admissionCurrent"`
	RescanPassed     int  `json:"rescanPassed"`
	RescanFailed     int  `json:"rescanFailed"`
	Stale            int  `json:"stale"`
	Invalid          int  `json:"invalid"`
	Alert            bool `json:"alert"`
}

func (m *Manager) SecurityAssessmentStats(now time.Time) SecurityAssessmentStats {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	m.securityStatsMu.Lock()
	defer m.securityStatsMu.Unlock()
	if !m.securityStatsAt.IsZero() && now.Sub(m.securityStatsAt) < 30*time.Second {
		return m.securityStats
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return SecurityAssessmentStats{Invalid: 1, Alert: true}
	}
	_, entries := m.active.catalog.Entries()
	stats := SecurityAssessmentStats{Artifacts: len(entries)}
	for _, entry := range entries {
		if entry.SecurityAdmission == nil {
			stats.Unassessed++
			continue
		}
		chainRaw, err := m.active.signed.ReadSecurityStatusChain(entry.Ref)
		if err != nil {
			stats.Invalid++
			continue
		}
		records, err := artifactassessment.InspectStatusChain(chainRaw)
		if err != nil {
			stats.Invalid++
			continue
		}
		if len(records) == 0 {
			expiresAt, err := time.Parse(time.RFC3339Nano, entry.SecurityAdmission.ExpiresAt)
			if err != nil {
				stats.Invalid++
			} else if !expiresAt.After(now) {
				stats.Stale++
			} else {
				stats.AdmissionCurrent++
			}
			continue
		}
		latest, _, err := artifactassessment.InspectStatus(records[len(records)-1])
		if err != nil {
			stats.Invalid++
		} else if !latest.Evaluation.ExpiresAt.After(now) {
			stats.Stale++
		} else if latest.Evaluation.Decision == artifactassessment.DecisionPass {
			stats.RescanPassed++
		} else {
			stats.RescanFailed++
		}
	}
	stats.Alert = stats.RescanFailed > 0 || stats.Stale > 0 || stats.Invalid > 0
	m.securityStats, m.securityStatsAt = stats, now
	return stats
}

func (m *Manager) invalidateSecurityAssessmentStats() {
	m.securityStatsMu.Lock()
	m.securityStatsAt = time.Time{}
	m.securityStatsMu.Unlock()
}
