package repositoryruntime

import (
	"errors"
	"fmt"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
)

// WithdrawWorkspace stops future discovery and delivery of a source-produced
// candidate. Exact reads remain available for active generations, rollback
// and GC reference accounting.
func (m *Manager) WithdrawWorkspace(ref pluginv1.ArtifactRef, occurredAt time.Time) (catalog.Entry, uint64, error) {
	if m == nil || ref.Channel != "workspace" {
		return catalog.Entry{}, 0, errors.New("只有 workspace 制品可以撤回")
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return catalog.Entry{}, 0, errors.New("活动制品仓库不可用")
	}
	if mirror != nil {
		if _, _, err := mirror.catalog.WithdrawWorkspace(ref, occurredAt); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷 workspace 撤回失败: %w", err))
			return catalog.Entry{}, 0, errors.New("制品迁移观察卷不可用，workspace 撤回已冻结")
		}
	}
	entry, revision, err := active.catalog.WithdrawWorkspace(ref, occurredAt)
	if err != nil {
		if mirror != nil {
			m.recordMigrationError(fmt.Errorf("活动卷 workspace 撤回失败: %w", err))
		}
		return catalog.Entry{}, 0, err
	}
	return entry, revision, nil
}

func (m *Manager) RestoreWorkspace(ref pluginv1.ArtifactRef, occurredAt time.Time) (catalog.Entry, uint64, error) {
	if m == nil || ref.Channel != "workspace" {
		return catalog.Entry{}, 0, errors.New("只有 workspace 制品可以恢复")
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return catalog.Entry{}, 0, errors.New("活动制品仓库不可用")
	}
	if mirror != nil {
		if _, _, err := restoreWorkspaceInSet(mirror, ref, occurredAt); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷 workspace 恢复失败: %w", err))
			return catalog.Entry{}, 0, errors.New("制品迁移观察卷不可用，workspace 恢复已冻结")
		}
	}
	entry, revision, err := restoreWorkspaceInSet(active, ref, occurredAt)
	if err != nil {
		if mirror != nil {
			m.recordMigrationError(fmt.Errorf("活动卷 workspace 恢复失败: %w", err))
		}
		return catalog.Entry{}, 0, err
	}
	return entry, revision, nil
}

func restoreWorkspaceInSet(set *repositorySet, ref pluginv1.ArtifactRef, occurredAt time.Time) (catalog.Entry, uint64, error) {
	revision, _ := set.catalog.Entries()
	entry, found := set.catalog.Lookup(ref)
	if !found {
		return catalog.Entry{}, revision, errors.New("待恢复 workspace 制品不存在")
	}
	if entry.LifecycleStatus != catalog.LifecycleWithdrawn {
		return entry, revision, nil
	}
	return set.catalog.SetLifecycle(catalog.LifecycleRequest{
		Ref: ref, Status: catalog.LifecycleActive, Reason: "development source restored", ExpectedRevision: revision,
	}, occurredAt)
}
