package platformcontrol

import (
	"context"
	"errors"
	"sync"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	platformcontrolport "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

const (
	CodeProfileInvalid       = "platform_control.profile_invalid"
	CodeSecretUnavailable    = "platform_control.secret_unavailable"
	CodeDatabaseUnavailable  = "platform_control.database_unavailable"
	CodeInitializationFailed = "platform_control.initialization_failed"
	CodeCommitConflict       = "platform_control.commit_conflict"
)

type Bootstrapper = platformcontrolport.Bootstrapper
type ManagedStore = platformcontrolport.ManagedStore
type SecretSource = platformcontrolport.SecretSource

type Controller struct {
	profiles ProfileStore
	resolve  SecretResolver
	database Bootstrapper
	binding  *sharedstate.BindingStore

	mu      sync.RWMutex
	status  platformcontrolv1.Status
	profile *platformcontrolv1.Profile
}

func NewController(profiles ProfileStore, resolve SecretResolver, database Bootstrapper, binding *sharedstate.BindingStore) (*Controller, error) {
	if profiles == nil || resolve == nil || database == nil || binding == nil {
		return nil, errors.New("Platform Control Controller 依赖不能为空")
	}
	return &Controller{profiles: profiles, resolve: resolve, database: database, binding: binding, status: platformcontrolv1.Status{Phase: platformcontrolv1.PhaseUnconfigured}}, nil
}

func (c *Controller) Start(ctx context.Context) error {
	profile, err := c.profiles.Load(ctx)
	if err != nil {
		c.setStatus(platformcontrolv1.PhaseRecovery, 0, CodeProfileInvalid)
		return err
	}
	if profile == nil {
		c.setStatus(platformcontrolv1.PhaseUnconfigured, 0, "")
		return nil
	}
	c.setProfile(*profile)
	source, err := c.resolve(profile.SecretRef)
	if err != nil {
		c.setStatus(platformcontrolv1.PhaseRecovery, profile.Generation, CodeSecretUnavailable)
		return err
	}
	store, err := c.database.Open(ctx, *profile, source)
	if err != nil {
		c.setStatus(platformcontrolv1.PhaseRecovery, profile.Generation, CodeDatabaseUnavailable)
		return err
	}
	if err := c.binding.Bind(profile.Generation, platformcontrolport.ProfileIdentity(*profile), store); err != nil {
		_ = store.Close()
		c.setStatus(platformcontrolv1.PhaseRecovery, profile.Generation, CodeCommitConflict)
		return err
	}
	c.setStatus(platformcontrolv1.PhaseReady, profile.Generation, "")
	return nil
}

func (c *Controller) Configure(ctx context.Context, candidate platformcontrolv1.Profile, expectedGeneration uint64) error {
	if err := platformcontrolv1.ValidateProfile(candidate); err != nil || candidate.Generation != expectedGeneration+1 {
		c.setCandidateFailure(expectedGeneration, CodeProfileInvalid)
		if err != nil {
			return err
		}
		return ErrGenerationConflict
	}
	source, err := c.resolve(candidate.SecretRef)
	if err != nil {
		c.setCandidateFailure(expectedGeneration, CodeSecretUnavailable)
		return err
	}
	c.setStatus(platformcontrolv1.PhaseTesting, expectedGeneration, "")
	if err := c.database.Test(ctx, candidate, source); err != nil {
		c.setCandidateFailure(expectedGeneration, CodeDatabaseUnavailable)
		return err
	}
	c.setStatus(platformcontrolv1.PhaseInitializing, expectedGeneration, "")
	store, err := c.database.Initialize(ctx, candidate, source)
	if err != nil {
		c.setCandidateFailure(expectedGeneration, CodeInitializationFailed)
		return err
	}
	if err := c.profiles.Commit(ctx, candidate, expectedGeneration); err != nil {
		_ = store.Close()
		c.setCandidateFailure(expectedGeneration, CodeCommitConflict)
		return err
	}
	c.setProfile(candidate)
	if err := c.binding.Bind(candidate.Generation, platformcontrolport.ProfileIdentity(candidate), store); err != nil {
		_ = store.Close()
		c.setStatus(platformcontrolv1.PhaseRecovery, candidate.Generation, CodeCommitConflict)
		return err
	}
	c.setStatus(platformcontrolv1.PhaseReady, candidate.Generation, "")
	return nil
}

func (c *Controller) TestCandidate(ctx context.Context, candidate platformcontrolv1.Profile, expectedGeneration uint64) error {
	if err := platformcontrolv1.ValidateProfile(candidate); err != nil || candidate.Generation != expectedGeneration+1 {
		c.setCandidateFailure(expectedGeneration, CodeProfileInvalid)
		if err != nil {
			return err
		}
		return ErrGenerationConflict
	}
	source, err := c.resolve(candidate.SecretRef)
	if err != nil {
		c.setCandidateFailure(expectedGeneration, CodeSecretUnavailable)
		return err
	}
	c.setStatus(platformcontrolv1.PhaseTesting, expectedGeneration, "")
	if err := c.database.Test(ctx, candidate, source); err != nil {
		c.setCandidateFailure(expectedGeneration, CodeDatabaseUnavailable)
		return err
	}
	generation, _, ready := c.binding.Snapshot()
	if ready {
		c.setStatus(platformcontrolv1.PhaseReady, generation, "")
	} else {
		c.setStatus(platformcontrolv1.PhaseUnconfigured, 0, "")
	}
	return nil
}

var _ platformcontrolport.Administration = (*Controller)(nil)

func (c *Controller) setCandidateFailure(expectedGeneration uint64, code string) {
	generation, _, ready := c.binding.Snapshot()
	if ready && generation == expectedGeneration {
		c.setStatus(platformcontrolv1.PhaseReady, generation, code)
		return
	}
	c.setStatus(platformcontrolv1.PhaseRecovery, expectedGeneration, code)
}

func (c *Controller) Status() platformcontrolv1.Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	status := c.status
	if c.profile != nil {
		profile := *c.profile
		status.Profile = &profile
	}
	return status
}

func (c *Controller) setProfile(profile platformcontrolv1.Profile) {
	c.mu.Lock()
	copy := profile
	c.profile = &copy
	c.mu.Unlock()
}

func (c *Controller) setStatus(phase platformcontrolv1.Phase, generation uint64, code string) {
	c.mu.Lock()
	c.status = platformcontrolv1.Status{Phase: phase, Generation: generation, Code: code}
	c.mu.Unlock()
}
