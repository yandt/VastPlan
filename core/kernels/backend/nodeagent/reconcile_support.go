package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

func (r *Reconciler) Shutdown(ctx context.Context) error {
	if err := r.validate(); err != nil {
		return err
	}
	actual, err := r.StateStore.Load()
	if err != nil {
		return err
	}
	actual.Errors = nil
	for _, id := range r.Runtime.UnitIDs() {
		r.pulse()
		state, ok := actual.Units[id]
		if !ok {
			state = UnitState{Phase: PhaseActive, PhaseChangedAt: r.now()}
		}
		if err := r.setUnitPhase(&state, PhaseDraining); err != nil {
			return err
		}
		actual.Units[id] = state
		if err := r.checkpoint(&actual); err != nil {
			return err
		}
		if err := r.setUnitPhase(&state, PhaseDeactivating); err != nil {
			return err
		}
		actual.Units[id] = state
		if err := r.checkpoint(&actual); err != nil {
			return err
		}
		if err := r.Runtime.Stop(ctx, id); err != nil {
			actual.Errors = append(actual.Errors, OperationError{UnitID: id, Stage: "shutdown", Message: err.Error()})
			state.LastError = err.Error()
			if phaseErr := r.setUnitPhase(&state, PhaseFailed); phaseErr != nil {
				return phaseErr
			}
			actual.Units[id] = state
			if saveErr := r.checkpoint(&actual); saveErr != nil {
				return saveErr
			}
			continue
		}
		if err := r.setUnitPhase(&state, PhaseInstalledInactive); err != nil {
			return err
		}
		state.PIDs = nil
		state.StartedAt = nil
		actual.Units[id] = state
		if err := r.checkpoint(&actual); err != nil {
			return err
		}
	}
	// 进程退出后，即使实际态里留有历史 unit，本节点也不再满足当前期望态。
	actual.AppliedRevision = 0
	if err := r.checkpoint(&actual); err != nil {
		return err
	}
	if err := r.publishAssignmentReferences(ctx, 0, &actual, true); err != nil {
		return fmt.Errorf("释放 Assignment 制品引用失败: %w", err)
	}
	if len(actual.Errors) > 0 {
		return fmt.Errorf("节点 %s 关闭时有 %d 个操作失败", r.NodeID, len(actual.Errors))
	}
	return nil
}

func (r *Reconciler) prepare(ctx context.Context, unit deploymentv1.Unit) ([]InstalledPlugin, string, error) {
	plugins := make([]InstalledPlugin, 0, len(unit.Plugins))
	verifiedArtifacts := make([]VerifiedArtifact, 0, len(unit.Plugins))
	for _, ref := range unit.Plugins {
		r.pulse()
		if ref.SHA256 == "" {
			return nil, "download", fmt.Errorf("Assignment 缺少 %s@%s/%s 的精确 SHA-256", ref.ID, ref.Version, ref.Channel)
		}
		artifactRef := pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: ref.Channel}
		verified, err := r.resolveArtifact(ctx, artifactRef, ref.SHA256)
		r.pulse()
		if err != nil {
			return nil, "download", fmt.Errorf("读取 %s@%s/%s: %w", ref.ID, ref.Version, ref.Channel, err)
		}
		installed, err := r.Installer.Install(verified)
		r.pulse()
		if err != nil {
			return nil, "install", fmt.Errorf("安装 %s@%s/%s: %w", ref.ID, ref.Version, ref.Channel, err)
		}
		plugins = append(plugins, installed)
		verifiedArtifacts = append(verifiedArtifacts, verified)
	}
	if r.BootstrapUpgrade != nil {
		inventory, err := r.BootstrapUpgrade.Prepare(ctx, verifiedArtifacts)
		if err != nil {
			return nil, "bootstrap_upgrade_prepare", err
		}
		r.BootstrapInventory = &inventory
	}
	return plugins, "", nil
}

func (r *Reconciler) resolveArtifact(ctx context.Context, ref pluginv1.ArtifactRef, expectedSHA256 string) (VerifiedArtifact, error) {
	var notFound error
	for _, source := range r.Sources {
		if source == nil {
			return VerifiedArtifact{}, errors.New("制品源不能为空")
		}
		envelope, err := source.Fetch(ctx, ref)
		if errors.Is(err, artifacttrust.ErrNotFound) {
			notFound = errors.Join(notFound, fmt.Errorf("%s: %w", sourceName(source), err))
			continue
		}
		if err != nil {
			return VerifiedArtifact{}, fmt.Errorf("制品源 %s 失败: %w", sourceName(source), err)
		}
		verified, err := r.Verifier.Verify(ref, envelope)
		if err != nil {
			// 来源一旦返回内容，任何格式、摘要或证明失败都是安全事件；不得换源掩盖。
			return VerifiedArtifact{}, fmt.Errorf("制品源 %s 返回不可信内容: %w", sourceName(source), err)
		}
		if verified.Artifact().SHA256 != expectedSHA256 {
			return VerifiedArtifact{}, fmt.Errorf("制品源 %s 返回摘要 %s，与 Assignment 锁定摘要 %s 不一致", sourceName(source), verified.Artifact().SHA256, expectedSHA256)
		}
		return verified, nil
	}
	if notFound != nil {
		return VerifiedArtifact{}, fmt.Errorf("所有制品源均无此制品: %w", notFound)
	}
	return VerifiedArtifact{}, errors.New("没有可用制品源")
}

func (r *Reconciler) validate() error {
	if r.NodeID == "" {
		return errors.New("node id 不能为空")
	}
	if len(r.Sources) == 0 || !r.Verifier.configured || r.Installer == nil || r.Runtime == nil || r.StateStore == nil {
		return errors.New("reconciler 依赖未完整配置")
	}
	if r.RequireArtifactReferences && r.References == nil {
		return errors.New("托管制品源要求配置 Assignment 引用发布器")
	}
	if r.BootstrapUpgrade != nil && r.BootstrapInventory == nil {
		return errors.New("Bootstrap 自动升级要求配置初始 Inventory")
	}
	if r.BootstrapUpgrade != nil && r.BootstrapReferences == nil {
		return errors.New("Bootstrap 自动升级要求配置 Seed/LKG 引用发布器")
	}
	return nil
}

func installedBootstrapItems(actual ActualState) []bootstrapinventory.Item {
	items := make([]bootstrapinventory.Item, 0)
	for _, unit := range actual.Units {
		for _, plugin := range unit.Plugins {
			items = append(items, bootstrapinventory.Item{
				Ref:    pluginv1.ArtifactRef{PluginID: plugin.ID, Version: plugin.Version, Channel: plugin.Channel},
				SHA256: plugin.SHA256,
			})
		}
	}
	return items
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func sortedUnitIDs(units map[string]deploymentv1.Unit) []string {
	ids := make([]string, 0, len(units))
	for id := range units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func unionUnitIDs(units map[string]UnitState, runtimeIDs []string) []string {
	set := make(map[string]struct{}, len(units)+len(runtimeIDs))
	for id := range units {
		set[id] = struct{}{}
	}
	for _, id := range runtimeIDs {
		set[id] = struct{}{}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
