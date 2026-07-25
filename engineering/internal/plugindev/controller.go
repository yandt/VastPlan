package plugindev

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Controller struct {
	RepositoryRoot string
	Spec           Spec
	Target         string
	PollInterval   time.Duration
	Debounce       time.Duration
	Builder        Builder
	Publisher      Publisher
	Status         StatusWriter
	Logf           func(string, ...any)
	Now            func() time.Time
}

type buildResult struct {
	digest    string
	candidate Candidate
	err       error
}

type publishResult struct {
	digest    string
	candidate Candidate
	err       error
}

func (c *Controller) Run(ctx context.Context) error {
	if c.Builder == nil || c.Publisher == nil || c.Status == nil || c.Spec.ID == "" || c.Target == "" {
		return errors.New("Plugin Dev Controller 配置不完整")
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 300 * time.Millisecond
	}
	if c.Debounce <= 0 {
		c.Debounce = 600 * time.Millisecond
	}
	if c.Now == nil {
		c.Now = func() time.Time { return time.Now().UTC() }
	}
	if c.Logf == nil {
		c.Logf = func(string, ...any) {}
	}

	observed, err := SourceDigest(c.RepositoryRoot, c.Spec)
	if err != nil {
		return err
	}
	state := Status{SchemaVersion: 1, PluginID: c.Spec.ID, Target: c.Target, Phase: PhaseDebouncing, SourceDigest: observed, UpdatedAt: c.Now()}
	if err := c.Status.Write(state); err != nil {
		return err
	}
	pendingSince := c.Now().Add(-c.Debounce)
	attempted := ""
	var generation uint64
	var buildCancel context.CancelFunc
	buildResults := make(chan buildResult, 1)
	publishResults := make(chan publishResult, 1)
	building, publishing := false, false
	ticker := time.NewTicker(c.PollInterval)
	defer ticker.Stop()

	write := func(phase Phase, candidate Candidate, failure error) error {
		state.Generation, state.Phase, state.SourceDigest, state.UpdatedAt = generation, phase, observed, c.Now()
		state.LastError = ""
		if candidate.Version != "" {
			state.Version, state.PackageFile = candidate.Version, candidate.PackageFile
		}
		if failure != nil {
			state.LastError = failure.Error()
		}
		return c.Status.Write(state)
	}

	startBuild := func() error {
		generation++
		buildCtx, cancel := context.WithCancel(ctx)
		buildCancel = cancel
		digest, currentGeneration := observed, generation
		building = true
		if err := write(PhaseBuilding, Candidate{}, nil); err != nil {
			cancel()
			return err
		}
		c.Logf("workspace 候选开始构建 plugin=%s generation=%d digest=%s", c.Spec.ID, currentGeneration, digest[:16])
		go func() {
			candidate, buildErr := c.Builder.Build(buildCtx, c.Spec, digest, currentGeneration)
			buildResults <- buildResult{digest: digest, candidate: candidate, err: buildErr}
		}()
		return nil
	}

	for {
		if !building && !publishing && observed != attempted && !c.Now().Before(pendingSince.Add(c.Debounce)) {
			if err := startBuild(); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			if buildCancel != nil {
				buildCancel()
			}
			return nil
		case <-ticker.C:
			next, scanErr := SourceDigest(c.RepositoryRoot, c.Spec)
			if scanErr != nil {
				if err := write(PhaseFailed, Candidate{}, fmt.Errorf("扫描开发输入: %w", scanErr)); err != nil {
					return err
				}
				continue
			}
			if next == observed {
				continue
			}
			observed, pendingSince, attempted = next, c.Now(), ""
			if building && buildCancel != nil {
				buildCancel()
			}
			if !publishing {
				if err := write(PhaseDebouncing, Candidate{}, nil); err != nil {
					return err
				}
			}
			c.Logf("检测到插件输入变化 plugin=%s digest=%s", c.Spec.ID, next[:16])
		case result := <-buildResults:
			building, buildCancel = false, nil
			if result.err != nil {
				if errors.Is(result.err, context.Canceled) && result.digest != observed {
					continue
				}
				attempted = result.digest
				if err := write(PhaseFailed, result.candidate, result.err); err != nil {
					return err
				}
				c.Logf("workspace 候选构建失败 plugin=%s generation=%d: %v", c.Spec.ID, generation, result.err)
				continue
			}
			if result.digest != observed {
				continue
			}
			publishing = true
			if err := write(PhasePublishing, result.candidate, nil); err != nil {
				return err
			}
			go func(candidate Candidate, digest string) {
				publishResults <- publishResult{digest: digest, candidate: candidate, err: c.Publisher.Publish(ctx, candidate)}
			}(result.candidate, result.digest)
		case result := <-publishResults:
			publishing, attempted = false, result.digest
			if result.digest != observed {
				if result.err != nil {
					c.Logf("已过期 workspace 候选发布失败，继续处理最新输入 plugin=%s generation=%d: %v", c.Spec.ID, result.candidate.Generation, result.err)
				}
				if err := write(PhaseDebouncing, result.candidate, nil); err != nil {
					return err
				}
				continue
			}
			if result.err != nil {
				if err := write(PhaseFailed, result.candidate, result.err); err != nil {
					return err
				}
				c.Logf("workspace 候选发布失败 plugin=%s generation=%d: %v", c.Spec.ID, result.candidate.Generation, result.err)
				continue
			}
			if err := write(PhaseReady, result.candidate, nil); err != nil {
				return err
			}
			c.Logf("workspace 候选已就绪 plugin=%s version=%s generation=%d", c.Spec.ID, result.candidate.Version, result.candidate.Generation)
		}
	}
}

// MemoryStatusWriter is intentionally tiny and useful for deterministic
// controller tests; production uses FileStatusWriter.
type MemoryStatusWriter struct {
	mu     sync.Mutex
	Values []Status
}

func (w *MemoryStatusWriter) Write(value Status) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Values = append(w.Values, value)
	return nil
}

func (w *MemoryStatusWriter) Snapshot() []Status {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Status(nil), w.Values...)
}
