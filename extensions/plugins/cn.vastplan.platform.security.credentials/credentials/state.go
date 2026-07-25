package credentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/configurationauthority"
	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func validateManagedState(tenants map[string]map[string]ManagedRecord, audit map[string]managedAuditState, maintenance map[string]ManagedMaintenanceStatus) error {
	for tenantID, records := range tenants {
		if strings.TrimSpace(tenantID) == "" {
			return errors.New("托管凭证状态包含空 tenant")
		}
		for stageID, record := range records {
			if stageID != record.StageID || !strings.HasPrefix(stageID, "stage-") || !strings.HasPrefix(record.Ref.Handle, "credential://managed/") || record.Ref.Scope != "tenant" || record.Ref.Owner == "" || record.Ref.Purpose == "" || record.Resource == "" || record.Ref.Version < 1 {
				return fmt.Errorf("托管凭证状态 %q 元数据无效", stageID)
			}
			if record.Coordinator != "" && (record.Coordinator != configurationauthority.CoordinatorPluginID || !strings.HasPrefix(record.AuthorityID, configurationauthority.TokenPrefix) || !strings.HasPrefix(record.CandidateID, "pcfg_") || !strings.HasPrefix(record.ConfigurationID, "cfg_") || record.FieldID == "") {
				return fmt.Errorf("委托托管凭证状态 %q 元数据无效", stageID)
			}
			switch record.State {
			case managedPreparing, managedCandidate, managedActive:
				if record.Ciphertext == "" {
					return fmt.Errorf("托管凭证状态 %q 缺少密文", stageID)
				}
			case managedAborted, managedRetired:
				if record.Ciphertext != "" {
					return fmt.Errorf("已终止托管凭证 %q 不得保留密文", stageID)
				}
			default:
				return fmt.Errorf("托管凭证状态 %q 的 state 无效", stageID)
			}
		}
	}
	for tenantID, state := range audit {
		if strings.TrimSpace(tenantID) == "" || len(state.Events) > maximumManagedAuditEvents || (len(state.Events) > 0 && state.NextID < state.Events[len(state.Events)-1].ID) {
			return errors.New("托管凭证审计状态无效")
		}
		previousID := uint64(0)
		for _, event := range state.Events {
			if !managedAuditEventValid(event) || event.ID <= previousID {
				return errors.New("托管凭证审计事件无效")
			}
			previousID = event.ID
		}
	}
	for tenantID := range maintenance {
		if strings.TrimSpace(tenantID) == "" {
			return errors.New("托管凭证维护状态无效")
		}
	}
	return nil
}
func (s *Service) save() error {
	if s.session == nil {
		if s.testSave != nil {
			return s.testSave(s.data)
		}
		return errors.New("Credentials 写入缺少 Shared State 会话")
	}
	value, err := s.snapshotLocked(s.session.tenant)
	if err != nil {
		return err
	}
	revision, err := s.session.repository.save(s.session.ctx, s.session.call, value, s.session.revision)
	if err != nil {
		return err
	}
	s.session.revision = revision
	return nil
}

func (s *Service) snapshotLocked(tenantID string) (credentialSnapshot, error) {
	value := credentialSnapshot{
		Records: s.data.Tenants[tenantID], Managed: s.data.Managed[tenantID], Audit: s.data.ManagedAudit[tenantID], Maintenance: s.data.ManagedMaintenance[tenantID],
	}
	if value.Records == nil {
		value.Records = map[string]Record{}
	}
	if value.Managed == nil {
		value.Managed = map[string]ManagedRecord{}
	}
	if value.Audit.Events == nil {
		value.Audit.Events = []ManagedAuditEvent{}
	}
	if value.Maintenance.Counts == nil {
		value.Maintenance.Counts = managedStateCounts(value.Managed)
	}
	return value, validateCredentialSnapshot(value)
}

func (s *Service) installSnapshotLocked(tenantID string, value credentialSnapshot) {
	s.data = persisted{
		Tenants: map[string]map[string]Record{tenantID: value.Records}, Managed: map[string]map[string]ManagedRecord{tenantID: value.Managed},
		ManagedAudit: map[string]managedAuditState{tenantID: value.Audit}, ManagedMaintenance: map[string]ManagedMaintenanceStatus{tenantID: value.Maintenance},
	}
}

func (s *Service) withTenantState(ctx context.Context, host sdk.Host, call *contractv1.CallContext, work func() error) error {
	tenantID, err := tenant(call)
	if err != nil {
		return err
	}
	s.workflowMu.Lock()
	defer s.workflowMu.Unlock()
	if s.testSave != nil {
		return work()
	}
	repository, err := newCredentialStateRepository(host)
	if err != nil {
		return err
	}
	value, revision, err := repository.load(ctx, call)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.installSnapshotLocked(tenantID, value)
	s.session = &credentialStateSession{ctx: ctx, call: call, repository: repository, tenant: tenantID, revision: revision}
	maintenanceChanged := s.collectExpiredTenantLocked(tenantID, s.now().UTC(), false)
	if maintenanceChanged {
		err = s.save()
	}
	s.mu.Unlock()
	if err == nil {
		err = repository.collectOrphanChunks(ctx, call, s.now().UTC(), s.maintenance)
	}
	if err != nil {
		s.closeStateSession()
		return err
	}
	defer s.closeStateSession()
	return work()
}

func (s *Service) closeStateSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = nil
	s.data = persisted{Tenants: map[string]map[string]Record{}, Managed: map[string]map[string]ManagedRecord{}, ManagedAudit: map[string]managedAuditState{}, ManagedMaintenance: map[string]ManagedMaintenanceStatus{}}
}
func tenant(ctx *contractv1.CallContext) (string, error) {
	if ctx == nil || strings.TrimSpace(ctx.TenantId) == "" {
		return "", errors.New("凭证调用必须携带 tenant")
	}
	return ctx.TenantId, nil
}
func validName(name string) error {
	if strings.TrimSpace(name) == "" || len(name) > 160 {
		return errors.New("凭证 name 必须为 1-160 个非空字符")
	}
	return nil
}
func (s *Service) records(tenant string) map[string]Record {
	if s.data.Tenants[tenant] == nil {
		s.data.Tenants[tenant] = map[string]Record{}
	}
	return s.data.Tenants[tenant]
}

func (s *Service) managedRecords(tenant string) map[string]ManagedRecord {
	if s.data.Managed[tenant] == nil {
		s.data.Managed[tenant] = map[string]ManagedRecord{}
	}
	return s.data.Managed[tenant]
}

func managedOwner(call *contractv1.CallContext) (string, error) {
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || strings.TrimSpace(call.GetCaller().GetId()) == "" {
		return "", errors.New("托管凭证只接受已认证业务插件")
	}
	return call.GetCaller().GetId(), nil
}

func opaqueID(prefix string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw), nil
}

// StageManaged creates a non-runnable credential candidate owned by the
// authenticated calling plugin. owner is never accepted from the payload.
