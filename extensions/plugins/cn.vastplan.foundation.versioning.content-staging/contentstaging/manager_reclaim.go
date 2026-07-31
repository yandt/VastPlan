package contentstaging

import (
	"context"
	"errors"
	"time"

	contentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioncontent/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
)

func (m *Manager) Reclaim(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	var combined error
	for id, record := range m.protections {
		if record.Protection.State == contentv1.StatePrepared && record.Protection.ExpiresAt != nil && !m.now().Before(*record.Protection.ExpiresAt) {
			before := cloneProtectionRecord(*record)
			record.Protection.State = contentv1.StateExpired
			record.Protection.Revision++
			record.Protection.UpdatedAt = m.now().UTC()
			record.Protection.ExpiresAt = nil
			if err := m.provider.SaveProtection(ctx, *record); err != nil {
				*record = before
				combined = errors.Join(combined, err)
				continue
			}
			count++
		}
		if protectionTerminal(record.Protection.State) && m.now().Sub(record.Protection.UpdatedAt) >= m.limits.TerminalRetention {
			if err := m.provider.DeleteProtection(ctx, record.Owner, id); err != nil {
				combined = errors.Join(combined, err)
				continue
			}
			delete(m.protections, id)
			count++
		}
	}
	for id, record := range m.uploads {
		if protectedState(record.Upload.State) && !m.now().Before(record.Upload.LeaseExpiresAt) {
			wasReady := record.Upload.State == stagingv1.StateReady
			before := cloneRecord(*record)
			record.Upload.State = stagingv1.StateExpired
			record.Upload.Revision++
			record.Upload.UpdatedAt = m.now()
			record.Content = nil
			record.FailureCode = ""
			if err := m.saveOrRollbackLocked(ctx, record, before); err != nil {
				combined = errors.Join(combined, err)
				continue
			}
			count++
			if err := m.provider.RemoveStaged(ctx, record.Owner, id); err != nil {
				combined = errors.Join(combined, err)
			}
			if wasReady {
				if err := m.removeUnreferencedContentLocked(ctx, record.Owner, before.Upload.ExpectedDigest); err != nil {
					combined = errors.Join(combined, err)
				}
			}
		}
		if terminalState(record.Upload.State) && m.now().Sub(record.Upload.UpdatedAt) >= m.limits.TerminalRetention {
			if err := m.provider.RemoveStaged(ctx, record.Owner, id); err != nil {
				combined = errors.Join(combined, err)
				continue
			}
			if record.Written && record.ActualDigest != "" {
				if err := m.removeUnreferencedContentLocked(ctx, record.Owner, record.ActualDigest); err != nil {
					combined = errors.Join(combined, err)
					continue
				}
			}
			if err := m.provider.DeleteUpload(ctx, record.Owner, id); err != nil {
				combined = errors.Join(combined, err)
				continue
			}
			delete(m.uploads, id)
			count++
		}
	}
	if combined != nil {
		return count, stagingError(stagingv1.ErrorStorageUnavailable, true, combined)
	}
	return count, nil
}

func (m *Manager) expireIfNeededLocked(ctx context.Context, record *uploadRecord) (bool, error) {
	if !protectedState(record.Upload.State) || m.now().Before(record.Upload.LeaseExpiresAt) {
		return false, nil
	}
	wasReady := record.Upload.State == stagingv1.StateReady
	before := cloneRecord(*record)
	record.Upload.State = stagingv1.StateExpired
	record.Upload.Revision++
	record.Upload.UpdatedAt = m.now()
	record.Content = nil
	record.FailureCode = ""
	if err := m.saveOrRollbackLocked(ctx, record, before); err != nil {
		return false, err
	}
	if err := m.provider.RemoveStaged(ctx, record.Owner, record.Upload.ID); err != nil {
		return true, stagingError(stagingv1.ErrorStorageUnavailable, true, err)
	}
	if wasReady {
		if err := m.removeUnreferencedContentLocked(ctx, record.Owner, before.Upload.ExpectedDigest); err != nil {
			return true, stagingError(stagingv1.ErrorStorageUnavailable, true, err)
		}
	}
	return true, nil
}

func (m *Manager) removeUnreferencedContentLocked(ctx context.Context, scope Scope, digest string) error {
	for _, protection := range m.protections {
		if protection.Owner.TenantID != scope.TenantID || !protectionActive(protection.Protection.State) {
			continue
		}
		for _, entry := range protection.Protection.Entries {
			if entry.Digest == digest {
				return nil
			}
		}
	}
	for _, candidate := range m.uploads {
		if candidate.Owner.TenantID == scope.TenantID && candidate.Upload.State == stagingv1.StateReady && candidate.Content != nil && candidate.Content.Digest == digest {
			return nil
		}
	}
	return m.provider.RemoveContent(ctx, scope, digest)
}

func (m *Manager) RunReclaimer(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = m.Reclaim(ctx)
		}
	}
}
