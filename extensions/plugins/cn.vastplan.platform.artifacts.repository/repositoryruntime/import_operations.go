package repositoryruntime

import (
	"errors"
	"fmt"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

// ImportRemote stores an exact artifact already selected by a remote Catalog.
// It bypasses local stable-promotion workflow but never bypasses publisher,
// content, provenance, assessment, quota or immutable-ref verification.
func (m *Manager) ImportRemote(source artifactrepositoryv1.Receipt, envelope artifacttrust.Envelope) (pluginv1.Artifact, error) {
	if m == nil || source.Protocol != artifactrepositoryv1.ProtocolRemote {
		return pluginv1.Artifact{}, errors.New("Local Plugin Library 导入缺少远端来源")
	}
	ref := pluginv1.ArtifactRef{PluginID: envelope.Artifact.PluginID, Version: envelope.Artifact.Version, Channel: envelope.Artifact.Channel}
	if err := artifactrepositoryv1.ValidateReceiptShape(source); err != nil || source.Ref != ref || source.SHA256 != envelope.Artifact.SHA256 {
		return pluginv1.Artifact{}, errors.New("远端 Catalog 回执与待导入制品不一致")
	}
	if err := m.trust.VerifyProof(envelope); err != nil {
		return pluginv1.Artifact{}, fmt.Errorf("远端导入制品不可信: %w", err)
	}
	if err := m.supplyChain.admit(envelope.Artifact); err != nil {
		return pluginv1.Artifact{}, err
	}
	if err := m.requireAdmissionReports(envelope.SecurityAdmission); err != nil {
		return pluginv1.Artifact{}, err
	}
	statusRecords, err := artifactassessment.InspectStatusChain(envelope.SecurityStatusChain)
	if err != nil {
		return pluginv1.Artifact{}, err
	}
	for _, raw := range statusRecords {
		if err := m.requireStatusReports(raw); err != nil {
			return pluginv1.Artifact{}, err
		}
	}

	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	active, mirror := m.active, m.mirror
	m.mu.RUnlock()
	if active == nil {
		return pluginv1.Artifact{}, errors.New("活动制品仓库不可用")
	}
	if prior, ok := active.catalog.Lookup(ref); ok && active.gc.IsRetired(prior.Ref, prior.SHA256) {
		return pluginv1.Artifact{}, errors.New("已进入 GC retirement 的不可变 ref 禁止重新导入")
	}
	if err := m.admitPublish(envelope.Artifact); err != nil {
		return pluginv1.Artifact{}, err
	}
	if mirror != nil {
		if _, err := importIntoSet(mirror, source, envelope, statusRecords); err != nil {
			m.recordMigrationError(fmt.Errorf("观察卷远端导入失败: %w", err))
			return pluginv1.Artifact{}, errors.New("制品迁移观察卷不可用，远端导入已冻结")
		}
	}
	artifact, err := importIntoSet(active, source, envelope, statusRecords)
	if err != nil {
		if mirror != nil {
			m.recordMigrationError(fmt.Errorf("活动卷远端导入失败: %w", err))
		}
		return pluginv1.Artifact{}, err
	}
	m.invalidateSecurityAssessmentStats()
	return artifact, nil
}

func importIntoSet(set *repositorySet, source artifactrepositoryv1.Receipt, envelope artifacttrust.Envelope, statusRecords [][]byte) (pluginv1.Artifact, error) {
	artifact, err := set.importWithSupplyChain(source, envelope)
	if err != nil {
		return pluginv1.Artifact{}, err
	}
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	for _, raw := range statusRecords {
		record, _, err := artifactassessment.InspectStatus(raw)
		if err != nil {
			return pluginv1.Artifact{}, err
		}
		if _, _, err := set.signed.AppendSecurityStatus(ref, raw, record.Evaluation.EvaluatedAt); err != nil {
			return pluginv1.Artifact{}, fmt.Errorf("导入安全复扫状态 sequence=%d: %w", record.Sequence, err)
		}
	}
	return artifact, nil
}
