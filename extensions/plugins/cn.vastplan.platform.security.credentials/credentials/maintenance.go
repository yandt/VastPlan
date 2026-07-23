package credentials

import (
	"sort"
	"time"
)

type ManagedMaintenanceStatus struct {
	LastRunAt   *time.Time     `json:"lastRunAt,omitempty"`
	AutoAborted uint64         `json:"autoAborted"`
	Collected   uint64         `json:"collected"`
	Counts      map[string]int `json:"counts"`
}

func (s *Service) maintenanceStatusLocked(tenantID string) ManagedMaintenanceStatus {
	status := s.data.ManagedMaintenance[tenantID]
	status.Counts = managedStateCounts(s.data.Managed[tenantID])
	return status
}

func managedStateCounts(records map[string]ManagedRecord) map[string]int {
	counts := map[string]int{managedPreparing: 0, managedCandidate: 0, managedActive: 0, managedAborted: 0, managedRetired: 0}
	for _, record := range records {
		counts[record.State]++
	}
	return counts
}

func (s *Service) CollectExpiredManaged() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	previousManaged := cloneManagedTenants(s.data.Managed)
	previousAudit := cloneManagedAudit(s.data.ManagedAudit)
	previousMaintenance := cloneManagedMaintenance(s.data.ManagedMaintenance)
	tenantSet := make(map[string]struct{}, len(s.data.Managed)+len(s.data.ManagedAudit)+len(s.data.ManagedMaintenance))
	for tenantID := range s.data.Managed {
		tenantSet[tenantID] = struct{}{}
	}
	for tenantID := range s.data.ManagedAudit {
		tenantSet[tenantID] = struct{}{}
	}
	for tenantID := range s.data.ManagedMaintenance {
		tenantSet[tenantID] = struct{}{}
	}
	tenantIDs := make([]string, 0, len(tenantSet))
	for tenantID := range tenantSet {
		tenantIDs = append(tenantIDs, tenantID)
	}
	sort.Strings(tenantIDs)
	for _, tenantID := range tenantIDs {
		s.collectExpiredTenantLocked(tenantID, now, true)
	}
	if err := s.save(); err != nil {
		s.data.Managed, s.data.ManagedAudit, s.data.ManagedMaintenance = previousManaged, previousAudit, previousMaintenance
		return err
	}
	return nil
}

func (s *Service) collectExpiredTenantLocked(tenantID string, now time.Time, force bool) bool {
	status := s.data.ManagedMaintenance[tenantID]
	if !force && status.LastRunAt != nil && status.LastRunAt.Add(s.maintenance.Interval).After(now) {
		return false
	}
	records := s.data.Managed[tenantID]
	if records == nil {
		records = map[string]ManagedRecord{}
		s.data.Managed[tenantID] = records
	}
	stageIDs := make([]string, 0, len(records))
	for stageID := range records {
		stageIDs = append(stageIDs, stageID)
	}
	sort.Slice(stageIDs, func(left, right int) bool {
		a, b := records[stageIDs[left]], records[stageIDs[right]]
		if a.UpdatedAt.Equal(b.UpdatedAt) {
			return stageIDs[left] < stageIDs[right]
		}
		return a.UpdatedAt.Before(b.UpdatedAt)
	})
	processed := 0
	for _, stageID := range stageIDs {
		if processed >= s.maintenance.BatchSize {
			break
		}
		record := records[stageID]
		switch {
		case record.State == managedPreparing && !record.UpdatedAt.Add(s.maintenance.PreparingMaxAge).After(now):
			record.State, record.Ciphertext, record.UpdatedAt = managedAborted, "", now
			records[stageID] = record
			s.appendManagedAuditLocked(tenantID, "managed.auto-aborted", record, now)
			status.AutoAborted++
			processed++
		case record.State == managedAborted && !record.UpdatedAt.Add(s.maintenance.AbortedRetention).After(now):
			s.appendManagedAuditLocked(tenantID, "managed.collected", record, now)
			delete(records, stageID)
			status.Collected++
			processed++
		}
	}
	s.pruneManagedAuditLocked(tenantID, now)
	runAt := now
	status.LastRunAt = &runAt
	status.Counts = managedStateCounts(records)
	s.data.ManagedMaintenance[tenantID] = status
	return true
}

func (s *Service) pruneManagedAuditLocked(tenantID string, now time.Time) {
	state := s.managedAuditStateLocked(tenantID)
	threshold := now.Add(-s.maintenance.AuditRetention)
	retained := make([]ManagedAuditEvent, 0, len(state.Events))
	for _, event := range state.Events {
		if !event.OccurredAt.Before(threshold) {
			retained = append(retained, event)
		}
	}
	if overflow := len(retained) - maximumManagedAuditEvents; overflow > 0 {
		retained = retained[overflow:]
	}
	state.Events = retained
	if len(state.Events) == 0 && state.NextID == 0 {
		delete(s.data.ManagedAudit, tenantID)
		return
	}
	s.data.ManagedAudit[tenantID] = state
}

func cloneManagedTenants(source map[string]map[string]ManagedRecord) map[string]map[string]ManagedRecord {
	clone := make(map[string]map[string]ManagedRecord, len(source))
	for tenantID, records := range source {
		clone[tenantID] = make(map[string]ManagedRecord, len(records))
		for stageID, record := range records {
			clone[tenantID][stageID] = record
		}
	}
	return clone
}

func cloneManagedAudit(source map[string]managedAuditState) map[string]managedAuditState {
	clone := make(map[string]managedAuditState, len(source))
	for tenantID, state := range source {
		state.Events = append([]ManagedAuditEvent(nil), state.Events...)
		clone[tenantID] = state
	}
	return clone
}

func cloneManagedMaintenance(source map[string]ManagedMaintenanceStatus) map[string]ManagedMaintenanceStatus {
	clone := make(map[string]ManagedMaintenanceStatus, len(source))
	for tenantID, status := range source {
		status.Counts = cloneCounts(status.Counts)
		clone[tenantID] = status
	}
	return clone
}

func cloneCounts(source map[string]int) map[string]int {
	clone := make(map[string]int, len(source))
	for state, count := range source {
		clone[state] = count
	}
	return clone
}
