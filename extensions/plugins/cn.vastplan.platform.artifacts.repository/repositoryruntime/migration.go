package repositoryruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactstorage"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/garbagecollection"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/references"
)

func (m *Manager) ProviderMigrationRequest(migrationID, phase string) (string, artifactstorage.VolumeMigrationRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.state == nil || m.state.MigrationID != migrationID {
		return "", artifactstorage.VolumeMigrationRequest{}, errors.New("迁移不存在")
	}
	return m.state.Target.ProviderID, artifactstorage.VolumeMigrationRequest{MigrationID: migrationID, SourceVolumeID: m.state.Source.VolumeID, TargetVolumeID: m.state.Target.VolumeID, Phase: phase}, nil
}

func (m *Manager) Migration() MigrationView {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.migrationViewLocked(time.Now().UTC())
}

func (m *Manager) Prepare(result artifactstorage.VolumeMigrationResult) (MigrationView, error) {
	if result.Phase != artifactstorage.MigrationPrepare || !result.Ready || result.MigrationID == "" {
		return MigrationView{}, errors.New("存储 Provider 未返回可用的 prepare 结果")
	}
	if err := validateVolume(result.Source); err != nil {
		return MigrationView{}, err
	}
	if err := validateVolume(result.Target); err != nil {
		return MigrationView{}, err
	}
	if result.Source.ProviderID != result.Target.ProviderID || result.Source.VolumeID == result.Target.VolumeID {
		return MigrationView{}, errors.New("File v1 迁移必须使用同一 Provider 的不同 volume")
	}
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil || filepath.Clean(result.Source.MountPath) != m.active.root {
		return MigrationView{}, errors.New("迁移 source 不是当前活动仓库")
	}
	if m.state != nil && m.state.MigrationID != result.MigrationID && !terminal(m.state.Phase) {
		return MigrationView{}, errors.New("已有其他未完成的制品迁移")
	}
	state := &MigrationState{SchemaVersion: stateSchemaVersion, MigrationID: result.MigrationID, Phase: PhasePrepared, Source: result.Source, Target: result.Target, UpdatedAt: nowString()}
	if err := m.saveState(state); err != nil {
		return MigrationView{}, err
	}
	m.state = state
	return m.migrationViewLocked(time.Now().UTC()), nil
}

func (m *Manager) MarkSynced(result artifactstorage.VolumeMigrationResult) (MigrationView, error) {
	if result.Phase != artifactstorage.MigrationSync || !result.Ready || result.Digest == "" {
		return MigrationView{}, errors.New("存储 Provider 未返回已校验的同步结果")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.matchResultLocked(result); err != nil {
		return MigrationView{}, err
	}
	if m.state.Phase != PhasePrepared && m.state.Phase != PhaseSynced {
		return MigrationView{}, errors.New("当前迁移阶段不允许增量同步")
	}
	next := *m.state
	next.Phase, next.Files, next.Bytes, next.Digest, next.LastError, next.UpdatedAt = PhaseSynced, result.Files, result.Bytes, result.Digest, "", nowString()
	if err := m.saveState(&next); err != nil {
		return MigrationView{}, err
	}
	m.state = &next
	return m.migrationViewLocked(time.Now().UTC()), nil
}

// Cutover freezes publication only for the final provider sync and verified
// pointer swap. Reads continue on the immutable source until the short swap.
func (m *Manager) Cutover(ctx context.Context, migrationID string, observation time.Duration, finalSync func(context.Context) (artifactstorage.VolumeMigrationResult, error)) (MigrationView, error) {
	if observation < 0 || finalSync == nil {
		return MigrationView{}, errors.New("迁移观察时间或最终同步器无效")
	}
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.RLock()
	if m.state == nil || m.state.MigrationID != migrationID || (m.state.Phase != PhasePrepared && m.state.Phase != PhaseSynced) {
		m.mu.RUnlock()
		return MigrationView{}, errors.New("迁移不在可切换阶段")
	}
	targetVolume := m.state.Target
	m.mu.RUnlock()
	result, err := finalSync(ctx)
	if err != nil {
		m.recordMigrationError(err)
		return MigrationView{}, err
	}
	if result.Phase != artifactstorage.MigrationSync || !result.Ready || result.MigrationID != migrationID {
		err := errors.New("最终同步回执与迁移不一致")
		m.recordMigrationError(err)
		return MigrationView{}, err
	}
	m.mu.RLock()
	receiptErr := m.matchResultLocked(result)
	m.mu.RUnlock()
	if receiptErr != nil {
		m.recordMigrationError(receiptErr)
		return MigrationView{}, receiptErr
	}
	candidate, err := m.openSet(targetVolume.MountPath)
	if err != nil {
		m.recordMigrationError(err)
		return MigrationView{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !sameCatalog(m.active.catalog.Stats(), candidate.catalog.Stats()) {
		return MigrationView{}, errors.New("候选 volume 的 Catalog 与活动仓库不一致")
	}
	next := *m.state
	next.Phase, next.Files, next.Bytes, next.Digest, next.LastError = PhaseObserving, result.Files, result.Bytes, result.Digest, ""
	next.ObservationUntil, next.UpdatedAt = time.Now().UTC().Add(observation).Format(time.RFC3339Nano), nowString()
	if err := m.saveState(&next); err != nil {
		return MigrationView{}, err
	}
	m.mirror, m.mirrorVolume, m.active, m.activeVolume, m.state = m.active, m.activeVolume, candidate, targetVolume, &next
	return m.migrationViewLocked(time.Now().UTC()), nil
}

func (m *Manager) Rollback(migrationID string) (MigrationView, error) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil || m.state.MigrationID != migrationID {
		return MigrationView{}, errors.New("迁移不存在")
	}
	if m.state.Phase == PhasePrepared || m.state.Phase == PhaseSynced {
		next := *m.state
		next.Phase, next.LastError, next.UpdatedAt = PhaseRolledBack, "", nowString()
		if err := m.saveState(&next); err != nil {
			return MigrationView{}, err
		}
		m.state = &next
		return m.migrationViewLocked(time.Now().UTC()), nil
	}
	if m.state.Phase != PhaseObserving || m.mirror == nil {
		return MigrationView{}, errors.New("迁移已无法回滚")
	}
	next := *m.state
	next.Phase, next.LastError, next.UpdatedAt = PhaseRolledBack, "", nowString()
	if err := m.saveState(&next); err != nil {
		return MigrationView{}, err
	}
	m.active, m.activeVolume, m.mirror, m.mirrorVolume, m.state = m.mirror, m.mirrorVolume, nil, artifactstorage.Volume{}, &next
	return m.migrationViewLocked(time.Now().UTC()), nil
}

func (m *Manager) Finalize(migrationID string, now time.Time) (MigrationView, error) {
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil || m.state.MigrationID != migrationID || m.state.Phase != PhaseObserving || m.mirror == nil {
		return MigrationView{}, errors.New("迁移不在可确认的观察阶段")
	}
	until, err := time.Parse(time.RFC3339Nano, m.state.ObservationUntil)
	if err != nil || now.UTC().Before(until) {
		return MigrationView{}, errors.New("迁移观察窗口尚未结束")
	}
	if m.state.LastError != "" || !sameCatalog(m.active.catalog.Stats(), m.mirror.catalog.Stats()) {
		return MigrationView{}, errors.New("迁移观察期存在镜像错误或 Catalog 偏差")
	}
	next := *m.state
	next.Phase, next.UpdatedAt = PhaseFinalized, nowString()
	if err := m.saveState(&next); err != nil {
		return MigrationView{}, err
	}
	m.mirror, m.mirrorVolume, m.state = nil, artifactstorage.Volume{}, &next
	return m.migrationViewLocked(now.UTC()), nil
}

func (m *Manager) MarkReleased(migrationID string, result artifactstorage.VolumeReleaseResult) (MigrationView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil || m.state.MigrationID != migrationID || m.state.Phase != PhaseFinalized || !result.Released || result.VolumeID != m.state.Source.VolumeID {
		return MigrationView{}, errors.New("旧 volume release 回执与已确认迁移不一致")
	}
	if m.configuredProvider != m.state.Target.ProviderID || m.configuredVolumeID != m.state.Target.VolumeID {
		return MigrationView{}, errors.New("运行配置尚未指向目标 volume，拒绝回收源 volume")
	}
	next := *m.state
	next.Phase, next.UpdatedAt = PhaseReleased, nowString()
	if err := m.saveState(&next); err != nil {
		return MigrationView{}, err
	}
	m.state = &next
	return m.migrationViewLocked(time.Now().UTC()), nil
}

func (m *Manager) SourceReleaseRequest(migrationID string) (artifactstorage.VolumeReleaseRequest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.state == nil || m.state.MigrationID != migrationID || m.state.Phase != PhaseFinalized {
		return artifactstorage.VolumeReleaseRequest{}, errors.New("迁移尚未确认")
	}
	if m.configuredProvider != m.state.Target.ProviderID || m.configuredVolumeID != m.state.Target.VolumeID {
		return artifactstorage.VolumeReleaseRequest{}, errors.New("运行配置尚未指向目标 volume")
	}
	return artifactstorage.VolumeReleaseRequest{MigrationID: migrationID, VolumeID: m.state.Source.VolumeID, ExpectedHandle: m.state.Source.Handle}, nil
}

func (m *Manager) openSet(root string) (*repositorySet, error) {
	if err := validateRepositoryRoot(root); err != nil {
		return nil, err
	}
	local, err := artifactrepository.NewRepository(root)
	if err != nil {
		return nil, err
	}
	signed := &artifactrepository.SignedRepository{Local: local, Trust: m.trust}
	gcStore, err := garbagecollection.Open(root)
	if err != nil {
		return nil, err
	}
	if err := gcStore.Recover(local, time.Now().UTC()); err != nil {
		return nil, err
	}
	store, err := catalog.Open(root, signed, gcStore)
	if err != nil {
		return nil, err
	}
	referenceStore, err := references.Open(root)
	if err != nil {
		return nil, err
	}
	return &repositorySet{root: filepath.Clean(root), signed: signed, adapter: artifactrepository.HTTPRepositoryAdapter{Repository: signed}, catalog: store, refs: referenceStore, gc: gcStore}, nil
}

func (s *repositorySet) publish(attestationRaw, packageBytes []byte) (artifactrepository.Artifact, error) {
	return s.publishWithProvenance(attestationRaw, packageBytes, nil, nil)
}

func (s *repositorySet) publishWithProvenance(attestationRaw, packageBytes, provenanceRaw, verificationRaw []byte) (artifactrepository.Artifact, error) {
	return s.publishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, nil)
}

func (s *repositorySet) publishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, admissionRaw []byte) (artifactrepository.Artifact, error) {
	artifact, err := s.adapter.PublishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, admissionRaw)
	if err != nil {
		return artifactrepository.Artifact{}, err
	}
	if _, err := s.catalog.RecordPublished(artifact, attestationRaw, time.Now().UTC()); err != nil {
		return artifactrepository.Artifact{}, fmt.Errorf("记录发布流水账: %w", err)
	}
	return artifact, nil
}

func (m *Manager) matchResultLocked(result artifactstorage.VolumeMigrationResult) error {
	if m.state == nil || m.state.MigrationID != result.MigrationID || result.Source != m.state.Source || result.Target != m.state.Target {
		return errors.New("存储 Provider 回执与当前迁移不一致")
	}
	return nil
}

func (m *Manager) recordMigrationError(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return
	}
	next := *m.state
	next.LastError, next.UpdatedAt = err.Error(), nowString()
	if saveErr := m.saveState(&next); saveErr == nil {
		m.state = &next
	}
}

func (m *Manager) migrationViewLocked(now time.Time) MigrationView {
	if m.state == nil {
		return MigrationView{ConfiguredActive: true}
	}
	state := m.state
	view := MigrationView{MigrationID: state.MigrationID, Phase: state.Phase, SourceProvider: state.Source.ProviderID, SourceVolumeID: state.Source.VolumeID, TargetProvider: state.Target.ProviderID, TargetVolumeID: state.Target.VolumeID, Files: state.Files, Bytes: state.Bytes, Digest: state.Digest, ObservationUntil: state.ObservationUntil}
	if state.LastError != "" {
		view.LastError = publicMigrationError
	}
	view.ConfiguredActive = m.configuredProvider == state.Target.ProviderID && m.configuredVolumeID == state.Target.VolumeID
	view.CanRollback = state.Phase == PhasePrepared || state.Phase == PhaseSynced || state.Phase == PhaseObserving
	if state.Phase == PhaseObserving && state.LastError == "" {
		until, err := time.Parse(time.RFC3339Nano, state.ObservationUntil)
		view.CanFinalize = err == nil && !now.Before(until)
	}
	view.CanRelease = state.Phase == PhaseFinalized && view.ConfiguredActive
	return view
}

func (m *Manager) loadState() (*MigrationState, error) {
	raw, err := os.ReadFile(m.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state MigrationState
	if err := decodeStrict(raw, &state); err != nil {
		return nil, fmt.Errorf("解析仓库迁移状态: %w", err)
	}
	if state.SchemaVersion != stateSchemaVersion || state.MigrationID == "" || !validPhase(state.Phase) {
		return nil, errors.New("仓库迁移状态无效")
	}
	if err := validateVolume(state.Source); err != nil {
		return nil, err
	}
	if err := validateVolume(state.Target); err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *Manager) saveState(state *MigrationState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(m.statePath), ".repository-migration-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		return err
	}
	if err := errors.Join(temporary.Sync(), temporary.Close()); err != nil {
		return err
	}
	if err := os.Rename(name, m.statePath); err != nil {
		return err
	}
	committed = true
	return nil
}

func validateVolume(volume artifactstorage.Volume) error {
	if err := artifactstorage.ValidateProviderID(volume.ProviderID); err != nil {
		return err
	}
	if err := artifactstorage.ValidateVolumeID(volume.VolumeID); err != nil {
		return err
	}
	if volume.Handle == "" || volume.AccessMode != "filesystem" || !volume.Ready || volume.MountPath == "" || volume.Endpoint != "" {
		return errors.New("迁移 v1 只接受已就绪的 filesystem volume")
	}
	return validateRepositoryRoot(volume.MountPath)
}

func validateRepositoryRoot(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return errors.New("制品 volume mount path 必须是规范绝对路径")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("制品 volume 必须是仅属主可访问的非符号链接目录")
	}
	return nil
}

func ensureStateDirectory(directory string) error {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return errors.New("迁移状态目录必须是规范绝对路径")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("迁移状态目录必须仅属主可访问")
	}
	return nil
}

func decodeStrict(raw []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("JSON 只能包含一个值")
	}
	return nil
}

func sameCatalog(left, right catalog.Stats) bool {
	return left.Revision == right.Revision && left.Artifacts == right.Artifacts && left.InventorySHA256 != "" && left.InventorySHA256 == right.InventorySHA256 && left.PublicationRevision == right.PublicationRevision && left.PublicationInventorySHA256 != "" && left.PublicationInventorySHA256 == right.PublicationInventorySHA256
}

func validPhase(phase string) bool {
	switch phase {
	case PhasePrepared, PhaseSynced, PhaseObserving, PhaseFinalized, PhaseRolledBack, PhaseReleased:
		return true
	default:
		return false
	}
}

func terminal(phase string) bool {
	return phase == PhaseFinalized || phase == PhaseRolledBack || phase == PhaseReleased
}
func nowString() string { return time.Now().UTC().Format(time.RFC3339Nano) }
