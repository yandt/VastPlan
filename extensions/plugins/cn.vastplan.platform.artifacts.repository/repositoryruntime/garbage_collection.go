package repositoryruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/garbagecollection"
)

const minimumGCGracePeriod = 24 * time.Hour

type GCBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type GCCandidate struct {
	Ref       pluginv1.ArtifactRef `json:"ref"`
	SHA256    string               `json:"sha256"`
	Size      int64                `json:"size"`
	Lifecycle string               `json:"lifecycle"`
}

type GCPlan struct {
	SchemaVersion     string        `json:"schemaVersion"`
	PlanID            string        `json:"planId,omitempty"`
	Ready             bool          `json:"ready"`
	CreatedAt         time.Time     `json:"createdAt"`
	CatalogRevision   uint64        `json:"catalogRevision"`
	ReferenceRevision uint64        `json:"referenceRevision"`
	Candidates        []GCCandidate `json:"candidates"`
	Bytes             int64         `json:"bytes"`
	Blockers          []GCBlocker   `json:"blockers,omitempty"`
}

type GCStatus struct {
	Revision uint64                     `json:"revision"`
	Items    []garbagecollection.Record `json:"items"`
}

func (m *Manager) PlanGarbageCollection(now time.Time) GCPlan {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.planGarbageCollectionLocked(now)
}

func (m *Manager) QuarantineGarbageCollection(planID string, gracePeriod time.Duration, now time.Time) (GCStatus, error) {
	if gracePeriod < minimumGCGracePeriod {
		return GCStatus{}, fmt.Errorf("GC 隔离宽限期不得短于 %s", minimumGCGracePeriod)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return GCStatus{}, errors.New("活动制品仓库不可用")
	}
	if err := m.active.gc.Recover(m.active.signed.Local, now); err != nil {
		return GCStatus{}, err
	}
	plan := m.planGarbageCollectionLocked(now)
	if !plan.Ready || plan.PlanID == "" || plan.PlanID != planID {
		return GCStatus{}, errors.New("GC plan 已过期、存在阻断项或身份不匹配，请重新 plan")
	}
	for _, candidate := range plan.Candidates {
		record := garbagecollection.Record{
			RetirementID: plan.PlanID, Ref: candidate.Ref, SHA256: candidate.SHA256,
			Size: candidate.Size, Lifecycle: candidate.Lifecycle,
			QuarantinedAt: now.UTC(), SweepAfter: now.UTC().Add(gracePeriod),
		}
		if err := m.active.gc.BeginQuarantine(record); err != nil {
			return m.gcStatusLocked(), err
		}
		if err := m.active.signed.Local.QuarantineArtifact(candidate.Ref, candidate.SHA256, plan.PlanID); err != nil {
			return m.gcStatusLocked(), err
		}
		if err := m.active.gc.CompleteQuarantine(candidate.Ref, candidate.SHA256); err != nil {
			return m.gcStatusLocked(), err
		}
	}
	return m.gcStatusLocked(), nil
}

func (m *Manager) SweepGarbageCollection(now time.Time) (GCStatus, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return GCStatus{}, errors.New("活动制品仓库不可用")
	}
	if err := m.active.gc.Recover(m.active.signed.Local, now); err != nil {
		return GCStatus{}, err
	}
	health := m.active.refs.GCHealth(now)
	if !health.Ready || gcMigrationBlocked(m.state) {
		return m.gcStatusLocked(), errors.New("引用源不健康或仓库迁移未结束，GC sweep 已停止")
	}
	for _, record := range m.active.gc.Due(now) {
		if m.active.refs.IsProtected(record.Ref, record.SHA256) {
			return m.gcStatusLocked(), errors.New("隔离制品重新出现活动引用，GC sweep 已停止")
		}
		entry, ok := m.active.catalog.GarbageCandidate(record.Ref, record.SHA256)
		if !ok {
			return m.gcStatusLocked(), errors.New("隔离制品生命周期不再允许 GC sweep")
		}
		if (containsTarget(entry.Targets, "runner") && !m.active.refs.HasOwnerKind(artifactreference.OwnerRunnerInstall)) ||
			(containsTarget(entry.Targets, "mobile") && !m.active.refs.HasOwnerKind(artifactreference.OwnerMobileInstall)) {
			return m.gcStatusLocked(), errors.New("Runner 或 Mobile 安装引用源尚未接入，GC sweep 已停止")
		}
		if err := m.active.gc.BeginSweep(record.Ref, record.SHA256); err != nil {
			return m.gcStatusLocked(), err
		}
		if err := m.active.signed.Local.SweepArtifact(record.Ref, record.SHA256, record.RetirementID); err != nil {
			return m.gcStatusLocked(), err
		}
		if err := m.active.gc.CompleteSweep(record.Ref, record.SHA256, now); err != nil {
			return m.gcStatusLocked(), err
		}
	}
	return m.gcStatusLocked(), nil
}

func (m *Manager) GarbageCollectionStatus() GCStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gcStatusLocked()
}

func (m *Manager) planGarbageCollectionLocked(now time.Time) GCPlan {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	plan := GCPlan{SchemaVersion: "v1", Ready: true, CreatedAt: now.UTC(), Candidates: []GCCandidate{}}
	if m.active == nil {
		plan.Ready = false
		plan.Blockers = append(plan.Blockers, GCBlocker{Code: "repository_unavailable", Message: "活动制品仓库不可用"})
		return plan
	}
	health := m.active.refs.GCHealth(now)
	plan.ReferenceRevision = health.Revision
	if !health.Ready {
		plan.Ready = false
		if len(health.Missing) > 0 {
			plan.Blockers = append(plan.Blockers, GCBlocker{Code: "reference_source_missing", Message: "Seed 或 LKG 引用源缺失"})
		}
		if len(health.Expired) > 0 {
			plan.Blockers = append(plan.Blockers, GCBlocker{Code: "reference_source_expired", Message: "存在已过期的租约引用源"})
		}
	}
	if gcMigrationBlocked(m.state) {
		plan.Ready = false
		plan.Blockers = append(plan.Blockers, GCBlocker{Code: "migration_active", Message: "仓库存储迁移尚未完全结束"})
	}
	revision, entries := m.active.catalog.GarbageCandidates()
	plan.CatalogRevision = revision
	for _, entry := range entries {
		if m.active.refs.IsProtected(entry.Ref, entry.SHA256) || m.active.gc.IsRetired(entry.Ref, entry.SHA256) {
			continue
		}
		if containsTarget(entry.Targets, "runner") && !m.active.refs.HasOwnerKind(artifactreference.OwnerRunnerInstall) {
			plan.Ready = false
			plan.Blockers = appendUniqueGCBlocker(plan.Blockers, GCBlocker{Code: "runner_reference_source_missing", Message: "Runner 安装引用源尚未接入"})
			continue
		}
		if containsTarget(entry.Targets, "mobile") && !m.active.refs.HasOwnerKind(artifactreference.OwnerMobileInstall) {
			plan.Ready = false
			plan.Blockers = appendUniqueGCBlocker(plan.Blockers, GCBlocker{Code: "mobile_reference_source_missing", Message: "Mobile 安装引用源尚未接入"})
			continue
		}
		plan.Candidates = append(plan.Candidates, GCCandidate{Ref: entry.Ref, SHA256: entry.SHA256, Size: entry.Size, Lifecycle: entry.LifecycleStatus})
		plan.Bytes += entry.Size
	}
	if plan.Ready {
		plan.PlanID = gcPlanID(plan)
	}
	return plan
}

func containsTarget(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func appendUniqueGCBlocker(values []GCBlocker, blocker GCBlocker) []GCBlocker {
	for _, value := range values {
		if value.Code == blocker.Code {
			return values
		}
	}
	return append(values, blocker)
}

func (m *Manager) gcStatusLocked() GCStatus {
	if m.active == nil {
		return GCStatus{Items: []garbagecollection.Record{}}
	}
	state := m.active.gc.List()
	return GCStatus{Revision: state.Revision, Items: append([]garbagecollection.Record{}, state.Items...)}
}

func gcMigrationBlocked(state *MigrationState) bool {
	return state != nil && state.Phase != PhaseReleased && state.Phase != PhaseRolledBack
}

func gcPlanID(plan GCPlan) string {
	payload := struct {
		SchemaVersion   string        `json:"schemaVersion"`
		CatalogRevision uint64        `json:"catalogRevision"`
		Candidates      []GCCandidate `json:"candidates"`
	}{plan.SchemaVersion, plan.CatalogRevision, plan.Candidates}
	raw, _ := json.Marshal(payload)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
