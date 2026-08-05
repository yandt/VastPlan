package deploymentmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

var (
	errInstallationMigrationStale = errors.New("插件安装数据库迁移计划已变化，必须重新预览和审批")
	errInstallationBackupRequired = errors.New("签名数据库迁移缺少经策略验证的备份证据")
)

func initialMigrationState(impact plugininstallation.SchemaImpact, now string) plugininstallation.MigrationState {
	phase := "NotRequired"
	if impact.RequiresMigration {
		phase = "Planned"
	}
	return plugininstallation.MigrationState{Phase: phase, PlanDigest: impact.Digest, UpdatedAt: now}
}

func schemaActivationForRevision(state *tenantState, revision platformadminapi.ServiceRevision) (*recordstorev1.SchemaActivation, error) {
	var current deploymentpublication.DataModelCatalog
	hasCurrent := false
	for _, item := range state.Revisions {
		if item.Deployment == revision.Deployment && item.Active && item.ID != revision.ID {
			current, hasCurrent = item.DataModelCatalog, true
			break
		}
	}
	// Initial Seed publication is handled by the two-stage Platform Control
	// bootstrap before ordinary routing exists. Upgrade authorization starts
	// once a durable active service revision is available for comparison.
	if !hasCurrent {
		return nil, nil
	}
	impact := plugininstallation.BuildSchemaImpact(current, revision.DataModelCatalog)
	if !impact.RequiresMigration {
		return nil, nil
	}
	mode := recordstorev1.SchemaActivationApproved
	approvedBy := revision.ApprovedBy
	if revision.Preview.Resolution.DevelopmentMode {
		mode, approvedBy = recordstorev1.SchemaActivationAutomatic, "development-policy"
	} else if approvedBy == "" {
		return nil, errServiceState
	}
	candidateID := fmt.Sprintf("service-revision-%d", revision.ID)
	backupRef := ""
	for id, candidate := range state.InstallationCandidates {
		if candidate.ServiceRevisionID != revision.ID || candidate.CancelledAt != "" {
			continue
		}
		candidateID = id
		if candidate.Preview.Impact.Schema.Digest != impact.Digest || candidate.Migration.PlanDigest != impact.Digest {
			return nil, errInstallationMigrationStale
		}
		backupRef = candidate.Migration.BackupRef
		break
	}
	authorization := &recordstorev1.SchemaActivation{CandidateID: candidateID, PlanDigest: impact.Digest, Mode: mode, ApprovedBy: approvedBy}
	for _, change := range impact.Changes {
		switch change.Kind {
		case "none", "retained":
			continue
		case "create", "additive":
			authorization.Models = append(authorization.Models, recordstorev1.SchemaMigrationAuthorization{
				Model: change.To, Storage: change.Storage, Kind: change.Kind, AllowSafe: true,
			})
		case "signed":
			if backupRef == "" {
				return nil, fmt.Errorf("%w: %s/%s", errInstallationBackupRequired, change.ModelID, change.MigrationID)
			}
			authorization.Models = append(authorization.Models, recordstorev1.SchemaMigrationAuthorization{
				Model: change.To, Storage: change.Storage, Kind: change.Kind, MigrationID: change.MigrationID,
				AllowSigned: true, BackupRef: backupRef,
			})
		default:
			return nil, fmt.Errorf("DataModel %s 需要尚未提供的手工迁移", change.ModelID)
		}
	}
	return authorization, nil
}

func markInstallationMigration(state *tenantState, revisionID uint64, phase, message, now string) {
	for id, record := range state.InstallationCandidates {
		if record.ServiceRevisionID != revisionID {
			continue
		}
		record.Migration.Phase, record.Migration.Error, record.Migration.UpdatedAt = phase, message, now
		record.UpdatedAt = now
		state.InstallationCandidates[id] = record
		return
	}
}

func (s *Service) attachInstallationBackupEvidence(call *contractv1.CallContext, candidateID string, impact plugininstallation.SchemaImpact, evidence map[string]json.RawMessage) error {
	if !impact.RequiresBackup {
		return nil
	}
	raw, ok := evidence["database.backup-ref"]
	if !ok {
		return errInstallationBackupRequired
	}
	var reference string
	if err := json.Unmarshal(raw, &reference); err != nil || len(reference) < 8 || len(reference) > 512 {
		return errInstallationBackupRequired
	}
	tenant, err := callTenant(call)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	record, ok := state.InstallationCandidates[candidateID]
	if !ok {
		return errNotFound
	}
	old := record
	record.Migration.BackupRef, record.Migration.UpdatedAt = reference, s.now().Format(time.RFC3339Nano)
	record.UpdatedAt = record.Migration.UpdatedAt
	state.InstallationCandidates[candidateID] = record
	if err := s.saveLocked(); err != nil {
		state.InstallationCandidates[candidateID] = old
		return err
	}
	return nil
}
