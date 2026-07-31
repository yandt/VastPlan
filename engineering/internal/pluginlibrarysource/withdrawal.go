package pluginlibrarysource

import (
	"context"
	"fmt"
	"sort"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func (c *Controller) finishPendingWithdrawals(ctx context.Context, state *State) error {
	ids := make([]string, 0)
	for sourceID, value := range state.Sources {
		if value.PendingWithdrawal != nil {
			ids = append(ids, sourceID)
		}
	}
	sort.Strings(ids)
	for _, sourceID := range ids {
		if err := c.withdraw(ctx, state, sourceID, *state.Sources[sourceID].PendingWithdrawal); err != nil {
			return err
		}
	}
	ids = ids[:0]
	for sourceID := range state.Sources {
		ids = append(ids, sourceID)
	}
	sort.Strings(ids)
	for _, sourceID := range ids {
		value := state.Sources[sourceID]
		if value.ActiveRef == nil || value.PluginID == "" {
			continue
		}
		candidates, err := c.Withdrawer.WorkspaceCandidates(ctx, value.PluginID)
		if err != nil {
			return err
		}
		for _, candidate := range candidates {
			if candidate.Ref == *value.ActiveRef {
				continue
			}
			if err := c.Withdrawer.WithdrawWorkspace(ctx, candidate.Ref); err != nil {
				return err
			}
			c.Logf("已清理插件的过期 workspace 候选 source=%s version=%s", sourceID, candidate.Ref.Version)
		}
	}
	return nil
}

func (c *Controller) adoptExistingCandidates(ctx context.Context, state *State) error {
	ids := make([]string, 0, len(state.Sources))
	for sourceID := range state.Sources {
		ids = append(ids, sourceID)
	}
	sort.Strings(ids)
	for _, sourceID := range ids {
		value := state.Sources[sourceID]
		if value.PluginID == "" || value.ActiveRef != nil {
			continue
		}
		candidates, err := c.Withdrawer.WorkspaceCandidates(ctx, value.PluginID)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			continue
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].Revision < candidates[j].Revision })
		active := candidates[len(candidates)-1].Ref
		value.ActiveRef, value.Phase, value.UpdatedAt = &active, PhaseReady, c.Now()
		state.Sources[sourceID] = value
		for _, stale := range candidates[:len(candidates)-1] {
			if err := c.Withdrawer.WithdrawWorkspace(ctx, stale.Ref); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) withdraw(ctx context.Context, state *State, sourceID string, ref pluginv1.ArtifactRef) error {
	value := state.Sources[sourceID]
	value.Phase, value.UpdatedAt = PhaseWithdrawing, c.Now()
	state.Sources[sourceID] = value
	if err := c.save(state); err != nil {
		return err
	}
	if err := c.Withdrawer.WithdrawWorkspace(ctx, ref); err != nil {
		return fmt.Errorf("撤回 %s@%s: %w", ref.PluginID, ref.Version, err)
	}
	value = state.Sources[sourceID]
	value.PendingWithdrawal, value.LastError, value.UpdatedAt = nil, "", c.Now()
	if value.ActiveRef == nil {
		value.Phase = PhaseRemoved
	} else {
		value.Phase = PhaseReady
	}
	state.Sources[sourceID] = value
	return c.save(state)
}

func (c *Controller) removeSource(ctx context.Context, state *State, sourceID string) error {
	value := state.Sources[sourceID]
	if value.PluginID != "" {
		candidates, err := c.Withdrawer.WorkspaceCandidates(ctx, value.PluginID)
		if err != nil {
			return err
		}
		for _, candidate := range candidates {
			if value.ActiveRef != nil && candidate.Ref == *value.ActiveRef {
				continue
			}
			if err := c.Withdrawer.WithdrawWorkspace(ctx, candidate.Ref); err != nil {
				return err
			}
		}
	}
	if value.ActiveRef != nil {
		ref := *value.ActiveRef
		value.ActiveRef, value.PendingWithdrawal = nil, &ref
		state.Sources[sourceID] = value
		if err := c.save(state); err != nil {
			return err
		}
		if err := c.withdraw(ctx, state, sourceID, ref); err != nil {
			return err
		}
	}
	value = state.Sources[sourceID]
	value.Phase, value.Fingerprint, value.SourceDigest, value.LastError, value.UpdatedAt = PhaseRemoved, "", "", "", c.Now()
	state.Sources[sourceID] = value
	delete(c.pending, sourceID)
	c.Logf("本地插件源码已撤回 source=%s plugin=%s", sourceID, value.PluginID)
	return c.save(state)
}
