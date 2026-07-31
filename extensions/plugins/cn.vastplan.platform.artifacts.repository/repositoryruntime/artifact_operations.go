package repositoryruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactstorage"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/references"
)

func (m *Manager) Publish(attestationRaw, packageBytes []byte) (pluginv1.Artifact, error) {
	return m.PublishWithProvenance(attestationRaw, packageBytes, nil, nil)
}

func (m *Manager) PublishWithProvenance(attestationRaw, packageBytes, provenanceRaw, verificationRaw []byte) (pluginv1.Artifact, error) {
	return m.PublishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, nil)
}

func (m *Manager) PublishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, admissionRaw []byte) (pluginv1.Artifact, error) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return pluginv1.Artifact{}, errors.New("活动制品仓库不可用")
	}
	var attestation artifactrepository.Attestation
	if err := json.Unmarshal(attestationRaw, &attestation); err != nil {
		return pluginv1.Artifact{}, errors.New("解析待发布制品证明失败")
	}
	ref := pluginv1.ArtifactRef{PluginID: attestation.Artifact.PluginID, Version: attestation.Artifact.Version, Channel: attestation.Artifact.Channel}
	if prior, ok := active.catalog.Lookup(ref); ok && active.gc.IsRetired(prior.Ref, prior.SHA256) {
		return pluginv1.Artifact{}, errors.New("已进入 GC retirement 的不可变 ref 禁止重新发布")
	}
	if err := m.admitPublish(attestation.Artifact); err != nil {
		return pluginv1.Artifact{}, err
	}
	if err := m.supplyChain.admit(attestation.Artifact); err != nil {
		return pluginv1.Artifact{}, err
	}
	if err := m.requireAdmissionReports(admissionRaw); err != nil {
		return pluginv1.Artifact{}, err
	}
	now := time.Now().UTC()
	publicationID, err := active.catalog.AuthorizePublicationWithSupplyChain(attestation, provenanceRaw, verificationRaw, admissionRaw, now)
	if err != nil {
		return pluginv1.Artifact{}, err
	}
	if mirror != nil {
		mirrorPublicationID, authorizeErr := mirror.catalog.AuthorizePublicationWithSupplyChain(attestation, provenanceRaw, verificationRaw, admissionRaw, now)
		if authorizeErr != nil || mirrorPublicationID != publicationID {
			m.recordMigrationError(errors.New("观察卷发布审批状态不一致"))
			return pluginv1.Artifact{}, errors.New("制品迁移观察卷审批状态不一致，发布已冻结")
		}
		if _, err := mirror.publishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, admissionRaw); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷镜像发布失败: %w", err))
			return pluginv1.Artifact{}, errors.New("制品迁移观察卷不可用，发布已冻结")
		}
		if err := mirror.catalog.MarkPublicationPublishedWithSupplyChain(mirrorPublicationID, attestationRaw, provenanceRaw, verificationRaw, admissionRaw, now); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷发布审批提交失败: %w", err))
			return pluginv1.Artifact{}, errors.New("制品迁移观察卷审批状态不可用，发布已冻结")
		}
	}
	artifact, err := active.publishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, admissionRaw)
	if err != nil {
		if mirror != nil {
			m.recordMigrationError(fmt.Errorf("候选卷发布失败: %w", err))
		}
		return pluginv1.Artifact{}, err
	}
	if err := active.catalog.MarkPublicationPublishedWithSupplyChain(publicationID, attestationRaw, provenanceRaw, verificationRaw, admissionRaw, now); err != nil {
		if mirror != nil {
			m.recordMigrationError(fmt.Errorf("活动卷发布审批提交失败: %w", err))
		}
		return pluginv1.Artifact{}, fmt.Errorf("提交发布审批状态: %w", err)
	}
	m.invalidateSecurityAssessmentStats()
	return artifact, nil
}

func (m *Manager) SubmitPublication(request catalog.PublicationRequest, actor string, occurredAt time.Time) (catalog.Publication, uint64, error) {
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return catalog.Publication{}, 0, errors.New("活动制品仓库不可用")
	}
	expiresAt := occurredAt.Add(m.publication.approvalTTL())
	if mirror != nil {
		if _, _, err := mirror.catalog.SubmitPublication(request, actor, occurredAt, expiresAt); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷发布申请镜像失败: %w", err))
			return catalog.Publication{}, 0, errors.New("制品迁移观察卷不可用，发布申请已冻结")
		}
	}
	record, revision, err := active.catalog.SubmitPublication(request, actor, occurredAt, expiresAt)
	if err != nil && mirror != nil {
		m.recordMigrationError(fmt.Errorf("活动卷发布申请失败: %w", err))
	}
	return record, revision, err
}

func (m *Manager) RejectPublication(request catalog.PublicationTransitionRequest, actor string, occurredAt time.Time) (catalog.Publication, uint64, error) {
	return m.terminatePublication(request, actor, occurredAt, false)
}

func (m *Manager) CancelPublication(request catalog.PublicationTransitionRequest, actor string, occurredAt time.Time) (catalog.Publication, uint64, error) {
	return m.terminatePublication(request, actor, occurredAt, true)
}

func (m *Manager) terminatePublication(request catalog.PublicationTransitionRequest, actor string, occurredAt time.Time, cancel bool) (catalog.Publication, uint64, error) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return catalog.Publication{}, 0, errors.New("活动制品仓库不可用")
	}
	transition := func(set *repositorySet) (catalog.Publication, uint64, error) {
		if cancel {
			return set.catalog.CancelPublication(request, actor, occurredAt)
		}
		return set.catalog.RejectPublication(request, actor, occurredAt)
	}
	if mirror != nil {
		if _, _, err := transition(mirror); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷发布审批终止失败: %w", err))
			return catalog.Publication{}, 0, errors.New("制品迁移观察卷不可用，发布审批终止已冻结")
		}
	}
	record, revision, err := transition(active)
	if err != nil && mirror != nil {
		m.recordMigrationError(fmt.Errorf("活动卷发布审批终止失败: %w", err))
	}
	return record, revision, err
}

func (m *Manager) ApprovePublication(request catalog.PublicationApprovalRequest, actor string, occurredAt time.Time) (catalog.Publication, uint64, error) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return catalog.Publication{}, 0, errors.New("活动制品仓库不可用")
	}
	if mirror != nil {
		if _, _, err := mirror.catalog.ApprovePublication(request, actor, occurredAt); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷发布批准镜像失败: %w", err))
			return catalog.Publication{}, 0, errors.New("制品迁移观察卷不可用，发布批准已冻结")
		}
	}
	record, revision, err := active.catalog.ApprovePublication(request, actor, occurredAt)
	if err != nil && mirror != nil {
		m.recordMigrationError(fmt.Errorf("活动卷发布批准失败: %w", err))
	}
	return record, revision, err
}

func (m *Manager) Publications() (catalog.PublicationPage, error) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return catalog.PublicationPage{}, errors.New("活动制品仓库不可用")
	}
	now := time.Now().UTC()
	if mirror != nil {
		if _, err := mirror.catalog.ExpirePublications(now); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷发布审批过期收敛失败: %w", err))
			return catalog.PublicationPage{}, errors.New("制品迁移观察卷不可用，发布审批读取已冻结")
		}
	}
	if _, err := active.catalog.ExpirePublications(now); err != nil {
		return catalog.PublicationPage{}, err
	}
	return active.catalog.Publications(), nil
}

func (m *Manager) SupplyChainEvidence(ref pluginv1.ArtifactRef) (catalog.SupplyChainEvidence, error) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return catalog.SupplyChainEvidence{}, errors.New("活动制品仓库不可用")
	}
	now := time.Now().UTC()
	if mirror != nil {
		if _, err := mirror.catalog.ExpirePublications(now); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷供应链审批过期收敛失败: %w", err))
			return catalog.SupplyChainEvidence{}, errors.New("制品迁移观察卷不可用，供应链证据读取已冻结")
		}
	}
	if _, err := active.catalog.ExpirePublications(now); err != nil {
		return catalog.SupplyChainEvidence{}, err
	}
	return active.catalog.Evidence(ref)
}

func (m *Manager) SetLifecycle(request catalog.LifecycleRequest, occurredAt time.Time) (catalog.Entry, uint64, error) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return catalog.Entry{}, 0, errors.New("活动制品仓库不可用")
	}
	if entry, ok := active.catalog.Lookup(request.Ref); ok && active.gc.IsRetired(entry.Ref, entry.SHA256) && request.Status != catalog.LifecycleYanked && request.Status != catalog.LifecycleRevoked {
		return catalog.Entry{}, 0, errors.New("已进入 GC retirement 的制品不能恢复为可解析状态")
	}
	if mirror != nil {
		if _, _, err := mirror.catalog.SetLifecycle(request, occurredAt); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷生命周期镜像失败: %w", err))
			return catalog.Entry{}, 0, errors.New("制品迁移观察卷不可用，生命周期变更已冻结")
		}
	}
	entry, revision, err := active.catalog.SetLifecycle(request, occurredAt)
	if err != nil && mirror != nil {
		m.recordMigrationError(fmt.Errorf("候选卷生命周期变更失败: %w", err))
	}
	return entry, revision, err
}

func (m *Manager) PutReferences(tenantID, publisherID string, value pluginv1.ArtifactReferenceSnapshot, occurredAt time.Time) (references.Snapshot, uint64, error) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return references.Snapshot{}, 0, errors.New("活动制品仓库不可用")
	}
	for _, reference := range value.References {
		if active.gc.IsRetired(reference.Ref, reference.SHA256) {
			return references.Snapshot{}, 0, errors.New("引用快照包含已隔离或清扫的制品")
		}
	}
	if err := active.catalog.ValidateKnownReferences(value.References); err != nil {
		return references.Snapshot{}, 0, err
	}
	if mirror != nil {
		if err := mirror.catalog.ValidateKnownReferences(value.References); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷引用校验失败: %w", err))
			return references.Snapshot{}, 0, errors.New("制品迁移观察卷不可用，引用更新已冻结")
		}
		if _, _, err := mirror.refs.Put(tenantID, publisherID, value, occurredAt); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷引用镜像失败: %w", err))
			return references.Snapshot{}, 0, errors.New("制品迁移观察卷不可用，引用更新已冻结")
		}
	}
	snapshot, revision, err := active.refs.Put(tenantID, publisherID, value, occurredAt)
	if err != nil && mirror != nil {
		m.recordMigrationError(fmt.Errorf("候选卷引用更新失败: %w", err))
	}
	return snapshot, revision, err
}

func (m *Manager) References() (uint64, []references.Snapshot) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active.refs.List()
}

func (m *Manager) activeRepositoryRoot() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil || m.active.root == "" {
		return "", errors.New("活动制品仓库不可用")
	}
	return m.active.root, nil
}

func (m *Manager) artifactProtected(ref pluginv1.ArtifactRef, sha256 string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active != nil && m.active.refs.IsProtected(ref, sha256)
}

func (m *Manager) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, []byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return pluginv1.Artifact{}, nil, nil, errors.New("活动制品仓库不可用")
	}
	if err := m.active.catalog.RequireDelivery(ref); err != nil {
		return pluginv1.Artifact{}, nil, nil, err
	}
	return m.active.adapter.Read(ref)
}

func (m *Manager) ReadWithAttestation(ref artifactrepository.Ref) (artifactrepository.Artifact, []byte, []byte, error) {
	return m.Read(ref)
}

func (m *Manager) ReadWithProvenance(ref artifactrepository.Ref) (artifactrepository.Artifact, []byte, []byte, []byte, []byte, error) {
	artifact, packageBytes, proof, provenance, verification, _, err := m.ReadWithSupplyChain(ref)
	return artifact, packageBytes, proof, provenance, verification, err
}

func (m *Manager) ReadWithSupplyChain(ref artifactrepository.Ref) (artifactrepository.Artifact, []byte, []byte, []byte, []byte, []byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return artifactrepository.Artifact{}, nil, nil, nil, nil, nil, errors.New("活动制品仓库不可用")
	}
	if err := m.active.catalog.RequireDelivery(ref); err != nil {
		return artifactrepository.Artifact{}, nil, nil, nil, nil, nil, err
	}
	return m.active.adapter.ReadWithSupplyChain(ref)
}

func (m *Manager) readRetainedWithSupplyChain(ref artifactrepository.Ref) (artifactrepository.Artifact, []byte, []byte, []byte, []byte, []byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil || ref.Channel != "workspace" {
		return artifactrepository.Artifact{}, nil, nil, nil, nil, nil, errors.New("保留制品读取只允许 workspace")
	}
	return m.active.adapter.ReadWithSupplyChain(ref)
}

func (m *Manager) ReadSecurityStatusChain(ref artifactrepository.Ref) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil {
		return nil, errors.New("活动制品仓库不可用")
	}
	if err := m.active.catalog.RequireDelivery(ref); err != nil {
		return nil, err
	}
	return m.active.signed.ReadSecurityStatusChain(ref)
}

func (m *Manager) readRetainedSecurityStatusChain(ref artifactrepository.Ref) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.active == nil || ref.Channel != "workspace" {
		return nil, errors.New("保留安全状态读取只允许 workspace")
	}
	return m.active.signed.ReadSecurityStatusChain(ref)
}

func (m *Manager) AppendSecurityStatus(ref artifactrepository.Ref, raw []byte, now time.Time) (*artifactassessment.StatusRecord, string, error) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return nil, "", errors.New("活动制品仓库不可用")
	}
	if err := m.requireStatusReports(raw); err != nil {
		return nil, "", err
	}
	if mirror != nil {
		mirrorRecord, mirrorDigest, err := mirror.signed.AppendSecurityStatus(ref, raw, now)
		if err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷安全复扫状态写入失败: %w", err))
			return nil, "", errors.New("制品迁移观察卷不可用，安全复扫状态写入已冻结")
		}
		record, digest, err := active.signed.AppendSecurityStatus(ref, raw, now)
		if err != nil || record.Sequence != mirrorRecord.Sequence || digest != mirrorDigest {
			m.recordMigrationError(errors.New("活动卷与观察卷安全复扫状态不一致"))
			return nil, "", errors.New("制品迁移双写安全复扫状态不一致")
		}
		m.invalidateSecurityAssessmentStats()
		return record, digest, nil
	}
	record, digest, err := active.signed.AppendSecurityStatus(ref, raw, now)
	if err == nil {
		m.invalidateSecurityAssessmentStats()
	}
	return record, digest, err
}

func (m *Manager) Query(query catalog.Query) catalog.Page {
	m.mu.RLock()
	defer m.mu.RUnlock()
	page := m.active.catalog.Query(query)
	for index := range page.Items {
		raw, err := m.active.signed.ReadSecurityStatusChain(page.Items[index].Ref)
		if err != nil || len(raw) == 0 {
			continue
		}
		records, err := artifactassessment.InspectStatusChain(raw)
		if err != nil || len(records) == 0 {
			continue
		}
		latest, digest, err := artifactassessment.InspectStatus(records[len(records)-1])
		if err != nil {
			continue
		}
		page.Items[index].SecurityStatus = &platformadminapi.ArtifactSecurityStatusEvidence{
			Sequence: latest.Sequence, RecordSHA256: digest, PreviousSHA256: latest.PreviousSHA256,
			Decision: latest.Evaluation.Decision, DatabaseRevision: latest.Evaluation.Scanner.DatabaseRevision,
			EvaluatedAt: latest.Evaluation.EvaluatedAt.Format(time.RFC3339Nano), ExpiresAt: latest.Evaluation.ExpiresAt.Format(time.RFC3339Nano),
			Critical: latest.Evaluation.Vulnerabilities.Critical, High: latest.Evaluation.Vulnerabilities.High,
			DeniedLicense: latest.Evaluation.Licenses.Denied, UnknownLicense: latest.Evaluation.Licenses.Unknown,
			VulnerabilityReportSHA256: latest.Evaluation.Vulnerabilities.ReportSHA256,
			LicenseReportSHA256:       latest.Evaluation.Licenses.ReportSHA256,
			Verification:              "verified",
		}
	}
	return page
}

func (m *Manager) Journal(after uint64, limit int) catalog.JournalPage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active.catalog.Journal(after, limit)
}

func (m *Manager) Resolve(request pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active.catalog.Resolve(request)
}

func (m *Manager) Stats() catalog.Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active.catalog.Stats()
}

func (m *Manager) ActiveVolume() artifactstorage.Volume {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeVolume
}
