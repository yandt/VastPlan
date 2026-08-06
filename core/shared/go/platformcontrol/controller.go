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
	profiles  ProfileStore
	resolve   SecretResolver
	database  Bootstrapper
	binding   *sharedstate.BindingStore
	materials SecretMaterialStore

	workflow sync.Mutex
	mu       sync.RWMutex
	status   platformcontrolv1.Status
	profile  *platformcontrolv1.Profile
}

func NewController(profiles ProfileStore, resolve SecretResolver, database Bootstrapper, binding *sharedstate.BindingStore, materials SecretMaterialStore) (*Controller, error) {
	if profiles == nil || resolve == nil || database == nil || binding == nil || materials == nil {
		return nil, errors.New("Platform Control Controller 依赖不能为空")
	}
	return &Controller{profiles: profiles, resolve: resolve, database: database, binding: binding, materials: materials, status: platformcontrolv1.Status{Phase: platformcontrolv1.PhaseUnconfigured}}, nil
}

func (c *Controller) Start(ctx context.Context) error {
	c.workflow.Lock()
	defer c.workflow.Unlock()

	profile, err := c.profiles.Load(ctx)
	if err != nil {
		c.setStatus(platformcontrolv1.PhaseRecovery, 0, CodeProfileInvalid)
		return err
	}
	if profile == nil {
		if err := c.materials.Reconcile(nil); err != nil {
			c.setStatus(platformcontrolv1.PhaseRecovery, 0, CodeSecretUnavailable)
			return err
		}
		c.setStatus(platformcontrolv1.PhaseUnconfigured, 0, "")
		return nil
	}
	// A committed profile is the durable boundary after which bootstrap state
	// must never become an authentication fallback, even if opening SQL fails.
	c.binding.RequireProvider()
	c.setProfile(*profile)
	if err := c.materials.Reconcile(&profile.SecretRef); err != nil {
		c.setStatus(platformcontrolv1.PhaseRecovery, profile.Generation, CodeSecretUnavailable)
		return err
	}
	source, err := c.resolveSecret(profile.SecretRef)
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

func (c *Controller) Configure(ctx context.Context, request platformcontrolv1.ChangeRequest) error {
	c.workflow.Lock()
	defer c.workflow.Unlock()

	if err := platformcontrolv1.ValidateChangeRequest(request); err != nil {
		c.setCandidateFailure(request.ExpectedGeneration, CodeProfileInvalid)
		return err
	}
	candidate, source, prepared, err := c.prepareCandidateSecret(ctx, request)
	if err != nil {
		c.setCandidateFailure(request.ExpectedGeneration, CodeSecretUnavailable)
		return err
	}
	persistedSecret := false
	if prepared != nil {
		defer func() {
			if !persistedSecret {
				_ = prepared.Rollback()
			}
		}()
	}
	expectedGeneration := request.ExpectedGeneration
	if candidate.Generation != expectedGeneration+1 {
		c.setCandidateFailure(expectedGeneration, CodeProfileInvalid)
		return ErrGenerationConflict
	}
	if source == nil {
		err = errors.New("Platform Control secret source 不可用")
	}
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
	if prepared != nil {
		if err := prepared.Commit(); err != nil {
			_ = store.Close()
			c.setCandidateFailure(expectedGeneration, CodeSecretUnavailable)
			return err
		}
		// Test/Initialize use the short-lived candidate path so an isolated
		// Database Runtime can reopen the secret from the wire Profile. Only
		// after the atomic rename may the durable Profile reference the final
		// owner-only path.
		candidate.SecretRef = prepared.Ref()
	}
	completeCommit := c.binding.BeginProviderCommit()
	if err := c.profiles.Commit(ctx, candidate, expectedGeneration); err != nil {
		completeCommit(false)
		_ = store.Close()
		c.setCandidateFailure(expectedGeneration, CodeCommitConflict)
		return err
	}
	completeCommit(true)
	persistedSecret = true
	c.setProfile(candidate)
	if err := c.binding.Bind(candidate.Generation, platformcontrolport.ProfileIdentity(candidate), store); err != nil {
		_ = store.Close()
		c.setStatus(platformcontrolv1.PhaseRecovery, candidate.Generation, CodeCommitConflict)
		return err
	}
	c.setStatus(platformcontrolv1.PhaseReady, candidate.Generation, "")
	return nil
}

func (c *Controller) TestCandidate(ctx context.Context, request platformcontrolv1.ChangeRequest) error {
	c.workflow.Lock()
	defer c.workflow.Unlock()

	if err := platformcontrolv1.ValidateChangeRequest(request); err != nil {
		c.setCandidateFailure(request.ExpectedGeneration, CodeProfileInvalid)
		return err
	}
	candidate, source, prepared, err := c.prepareCandidateSecret(ctx, request)
	if prepared != nil {
		defer prepared.Rollback()
	}
	expectedGeneration := request.ExpectedGeneration
	if err != nil {
		c.setCandidateFailure(expectedGeneration, CodeSecretUnavailable)
		return err
	}
	if candidate.Generation != expectedGeneration+1 {
		c.setCandidateFailure(expectedGeneration, CodeProfileInvalid)
		return ErrGenerationConflict
	}
	if source == nil {
		err = errors.New("Platform Control secret source 不可用")
	}
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
