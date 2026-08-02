package pluginlibrarysource

import (
	"context"
	"errors"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/engineering/internal/plugindev"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

func (c *Controller) publishChange(ctx context.Context, state *State, item Observation) error {
	current := state.Sources[item.SourceID]
	digest, err := plugindev.SourceDigest(c.RepositoryRoot, item.Spec)
	if err != nil {
		return err
	}
	if current.SourceDigest == digest && current.ActiveRef != nil {
		current.Fingerprint, current.Phase, current.LastError, current.UpdatedAt = item.Fingerprint, PhaseReady, "", c.Now()
		state.Sources[item.SourceID] = current
		return c.save(state)
	}
	state.Generation++
	current.SourceID, current.PluginID, current.Phase, current.LastError, current.UpdatedAt = item.SourceID, item.Spec.ID, PhaseBuilding, "", c.Now()
	state.Sources[item.SourceID] = current
	if err := c.save(state); err != nil {
		return err
	}
	candidate, err := c.Builder.Build(ctx, item.Spec, digest, state.Generation)
	if err != nil {
		return err
	}
	latest, err := Scan(c.RepositoryRoot)
	if err != nil {
		return err
	}
	if next, exists := latest[item.SourceID]; !exists || next.Err != nil || next.Fingerprint != item.Fingerprint {
		return errors.New("构建期间源码再次变化，丢弃过期候选")
	}
	current = state.Sources[item.SourceID]
	current.Phase, current.UpdatedAt = PhasePublishing, c.Now()
	state.Sources[item.SourceID] = current
	if err := c.save(state); err != nil {
		return err
	}
	if err := c.Publisher.Publish(ctx, candidate); err != nil {
		return err
	}
	ref := pluginv1.ArtifactRef{PluginID: candidate.PluginID, Version: candidate.Version, Channel: "workspace"}
	previous := cloneRef(current.ActiveRef)
	if c.IntentApplier != nil {
		action := plugininstallation.ActionUpgrade
		if previous == nil {
			action = plugininstallation.ActionInstall
		}
		intent := InstallationIntent{Action: action, PluginID: candidate.PluginID, Artifact: &ref}
		if err := intent.validate(); err != nil {
			return err
		}
		if err := c.IntentApplier.ApplyInstallationIntent(ctx, intent); err != nil {
			return err
		}
	}
	current.PluginID, current.Fingerprint, current.SourceDigest = candidate.PluginID, item.Fingerprint, digest
	current.ActiveRef, current.PendingWithdrawal = &ref, nil
	if previous != nil && *previous != ref {
		current.PendingWithdrawal = previous
	}
	current.Phase, current.LastError, current.UpdatedAt = PhaseReady, "", c.Now()
	state.Sources[item.SourceID] = current
	if err := c.save(state); err != nil {
		return err
	}
	if current.PendingWithdrawal != nil {
		if err := c.withdraw(ctx, state, item.SourceID, *current.PendingWithdrawal); err != nil {
			return err
		}
	}
	delete(c.pending, item.SourceID)
	c.Logf("本地插件库已更新 source=%s plugin=%s version=%s", item.SourceID, candidate.PluginID, candidate.Version)
	return nil
}
