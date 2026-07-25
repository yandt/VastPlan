// Package repositoryruntime owns the repository process' atomically swappable
// data plane. Storage providers copy physical volumes; this manager alone
// freezes publication, verifies the candidate catalog and changes visibility.
package repositoryruntime

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreport"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactstorage"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/garbagecollection"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/references"
)

const stateSchemaVersion = "v1"
const publicMigrationError = "迁移步骤失败，请查看服务端日志后重试"

const (
	PhasePrepared   = "prepared"
	PhaseSynced     = "synced"
	PhaseObserving  = "observing"
	PhaseFinalized  = "finalized"
	PhaseRolledBack = "rolled-back"
	PhaseReleased   = "released"
)

type MigrationState struct {
	SchemaVersion    string                 `json:"schemaVersion"`
	MigrationID      string                 `json:"migrationId"`
	Phase            string                 `json:"phase"`
	Source           artifactstorage.Volume `json:"source"`
	Target           artifactstorage.Volume `json:"target"`
	Files            int64                  `json:"files,omitempty"`
	Bytes            int64                  `json:"bytes,omitempty"`
	Digest           string                 `json:"digest,omitempty"`
	ObservationUntil string                 `json:"observationUntil,omitempty"`
	LastError        string                 `json:"lastError,omitempty"`
	UpdatedAt        string                 `json:"updatedAt"`
}

// MigrationView is safe for ordinary administration responses. Physical
// handles, mount paths and provider endpoints remain private process state.
type MigrationView struct {
	MigrationID      string `json:"migrationId,omitempty"`
	Phase            string `json:"phase,omitempty"`
	SourceProvider   string `json:"sourceProvider,omitempty"`
	SourceVolumeID   string `json:"sourceVolumeId,omitempty"`
	TargetProvider   string `json:"targetProvider,omitempty"`
	TargetVolumeID   string `json:"targetVolumeId,omitempty"`
	Files            int64  `json:"files,omitempty"`
	Bytes            int64  `json:"bytes,omitempty"`
	Digest           string `json:"digest,omitempty"`
	ObservationUntil string `json:"observationUntil,omitempty"`
	LastError        string `json:"lastError,omitempty"`
	ConfiguredActive bool   `json:"configuredActive"`
	CanRollback      bool   `json:"canRollback"`
	CanFinalize      bool   `json:"canFinalize"`
	CanRelease       bool   `json:"canRelease"`
}

type repositorySet struct {
	root    string
	signed  *pluginservice.SignedRepository
	adapter pluginservice.HTTPRepositoryAdapter
	catalog *catalog.Store
	refs    *references.Store
	gc      *garbagecollection.Store
}

type Manager struct {
	trust              *pluginservice.TrustStore
	statePath          string
	configuredProvider string
	configuredVolumeID string
	quota              QuotaPolicy
	publication        PublicationPolicy
	supplyChain        SupplyChainPolicy
	assessmentReports  *artifactreport.Archive

	publishMu    sync.Mutex
	mu           sync.RWMutex
	active       *repositorySet
	mirror       *repositorySet
	activeVolume artifactstorage.Volume
	mirrorVolume artifactstorage.Volume
	state        *MigrationState

	securityStatsMu sync.Mutex
	securityStats   SecurityAssessmentStats
	securityStatsAt time.Time
}

type Options struct {
	Quota             QuotaPolicy
	Publication       PublicationPolicy
	SupplyChain       SupplyChainPolicy
	AssessmentReports *artifactreport.Archive
}

func Open(initial artifactstorage.Volume, trust *pluginservice.TrustStore, statePath string, options ...Options) (*Manager, error) {
	if trust == nil || statePath == "" {
		return nil, errors.New("仓库迁移运行时必须配置信任根和状态文件")
	}
	if len(options) > 1 {
		return nil, errors.New("仓库运行时只能配置一组 Options")
	}
	var runtimeOptions Options
	if len(options) == 1 {
		runtimeOptions = options[0]
	}
	if err := runtimeOptions.Quota.Validate(); err != nil {
		return nil, err
	}
	if err := runtimeOptions.Publication.Validate(); err != nil {
		return nil, err
	}
	if err := runtimeOptions.SupplyChain.Validate(); err != nil {
		return nil, err
	}
	if err := validateVolume(initial); err != nil {
		return nil, fmt.Errorf("校验初始制品 volume: %w", err)
	}
	if err := ensureStateDirectory(filepath.Dir(filepath.Clean(statePath))); err != nil {
		return nil, err
	}
	manager := &Manager{trust: trust, statePath: filepath.Clean(statePath), configuredProvider: initial.ProviderID, configuredVolumeID: initial.VolumeID, quota: runtimeOptions.Quota, publication: runtimeOptions.Publication.normalized(), supplyChain: runtimeOptions.SupplyChain.normalized(), assessmentReports: runtimeOptions.AssessmentReports}
	state, err := manager.loadState()
	if err != nil {
		return nil, err
	}
	activeVolume := initial
	if state != nil {
		switch state.Phase {
		case PhaseObserving, PhaseFinalized, PhaseReleased:
			activeVolume = state.Target
		case PhasePrepared, PhaseSynced, PhaseRolledBack:
			activeVolume = state.Source
		}
	}
	active, err := manager.openSet(activeVolume.MountPath)
	if err != nil {
		return nil, fmt.Errorf("打开活动制品 volume: %w", err)
	}
	manager.active, manager.activeVolume, manager.state = active, activeVolume, state
	if state != nil && state.Phase == PhaseObserving {
		mirror, err := manager.openSet(state.Source.MountPath)
		if err != nil {
			return nil, fmt.Errorf("恢复迁移观察镜像: %w", err)
		}
		manager.mirror, manager.mirrorVolume = mirror, state.Source
	}
	return manager, nil
}
