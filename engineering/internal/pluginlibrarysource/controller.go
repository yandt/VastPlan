package pluginlibrarysource

import (
	"context"
	"errors"
	"sort"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/engineering/internal/plugindev"
)

type Withdrawer interface {
	WithdrawWorkspace(context.Context, pluginv1.ArtifactRef) error
	WorkspaceCandidates(context.Context, string) ([]artifactrepositoryv1.Receipt, error)
}

type Controller struct {
	RepositoryRoot string
	ScanInterval   time.Duration
	Debounce       time.Duration
	Builder        plugindev.Builder
	Publisher      plugindev.Publisher
	Withdrawer     Withdrawer
	Store          StateStore
	Now            func() time.Time
	Logf           func(string, ...any)

	pending map[string]pendingChange
}

type pendingChange struct {
	fingerprint string
	since       time.Time
}

func (c *Controller) Run(ctx context.Context) error {
	if c.RepositoryRoot == "" || c.Builder == nil || c.Publisher == nil || c.Withdrawer == nil || c.Store == nil {
		return errors.New("Local Plugin Library 源控制器配置不完整")
	}
	if c.ScanInterval <= 0 {
		c.ScanInterval = time.Second
	}
	if c.Debounce <= 0 {
		c.Debounce = 800 * time.Millisecond
	}
	if c.Now == nil {
		c.Now = func() time.Time { return time.Now().UTC() }
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}
	c.pending = map[string]pendingChange{}
	state, exists, err := c.Store.Load()
	if err != nil {
		return err
	}
	observed, err := Scan(c.RepositoryRoot)
	if err != nil {
		return err
	}
	if !exists || !state.Initialized {
		state = c.initialState(observed)
		if err := c.adoptExistingCandidates(ctx, &state); err != nil {
			return err
		}
		if err := c.Store.Save(state); err != nil {
			return err
		}
		c.Logf("Local Plugin Library 源基线已建立 plugins=%d；首次启动不批量构建", len(observed))
	} else if err := c.reconcile(ctx, &state, observed); err != nil {
		return err
	}
	ticker := time.NewTicker(c.ScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			observed, err := Scan(c.RepositoryRoot)
			if err != nil {
				c.Logf("扫描 Local Plugin Library 源失败: %v", err)
				continue
			}
			if err := c.reconcile(ctx, &state, observed); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				c.Logf("调和 Local Plugin Library 源失败: %v", err)
			}
		}
	}
}

func (c *Controller) initialState(observed map[string]Observation) State {
	now := c.Now()
	state := State{SchemaVersion: stateSchemaVersion, Initialized: true, Sources: make(map[string]SourceState, len(observed)), UpdatedAt: now}
	for sourceID, item := range observed {
		value := SourceState{SourceID: sourceID, PluginID: item.Spec.ID, Fingerprint: item.Fingerprint, Phase: PhaseObserved, UpdatedAt: now}
		if item.Err != nil {
			value.Phase, value.LastError = PhaseFailed, item.Err.Error()
		}
		state.Sources[sourceID] = value
	}
	return state
}

func (c *Controller) reconcile(ctx context.Context, state *State, observed map[string]Observation) error {
	if err := c.finishPendingWithdrawals(ctx, state); err != nil {
		return err
	}
	ids := make([]string, 0, len(observed))
	for sourceID := range observed {
		ids = append(ids, sourceID)
	}
	sort.Strings(ids)
	for _, sourceID := range ids {
		item := observed[sourceID]
		current, known := state.Sources[sourceID]
		if known && current.Fingerprint == item.Fingerprint && current.Phase != PhaseRemoved {
			delete(c.pending, sourceID)
			continue
		}
		if !c.debounced(sourceID, item.Fingerprint) {
			continue
		}
		if item.Err != nil {
			current.SourceID, current.PluginID, current.Fingerprint = sourceID, item.Spec.ID, item.Fingerprint
			current.Phase, current.LastError, current.UpdatedAt = PhaseFailed, item.Err.Error(), c.Now()
			state.Sources[sourceID] = current
			if err := c.save(state); err != nil {
				return err
			}
			continue
		}
		if err := c.publishChange(ctx, state, item); err != nil {
			current = state.Sources[sourceID]
			current.SourceID, current.PluginID, current.Fingerprint = sourceID, item.Spec.ID, item.Fingerprint
			current.Phase, current.LastError, current.UpdatedAt = PhaseFailed, err.Error(), c.Now()
			state.Sources[sourceID] = current
			if saveErr := c.save(state); saveErr != nil {
				return errors.Join(err, saveErr)
			}
			c.Logf("本地插件源码更新失败 source=%s: %v", sourceID, err)
		}
	}

	removed := make([]string, 0)
	for sourceID := range state.Sources {
		if _, exists := observed[sourceID]; !exists && state.Sources[sourceID].Phase != PhaseRemoved {
			removed = append(removed, sourceID)
		}
	}
	sort.Strings(removed)
	for _, sourceID := range removed {
		if !c.debounced(sourceID, "<removed>") {
			continue
		}
		if err := c.removeSource(ctx, state, sourceID); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) debounced(sourceID, fingerprint string) bool {
	now := c.Now()
	value, exists := c.pending[sourceID]
	if !exists || value.fingerprint != fingerprint {
		c.pending[sourceID] = pendingChange{fingerprint: fingerprint, since: now}
		return false
	}
	return !now.Before(value.since.Add(c.Debounce))
}

func (c *Controller) save(state *State) error {
	state.UpdatedAt = c.Now()
	return c.Store.Save(*state)
}

func cloneRef(value *pluginv1.ArtifactRef) *pluginv1.ArtifactRef {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
