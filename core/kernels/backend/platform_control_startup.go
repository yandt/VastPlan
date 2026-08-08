package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	kernelplatformcontrol "cdsoft.com.cn/VastPlan/core/shared/go/platformcontrol"
	platformcontrolport "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

var errPlatformControlNotReady = fmt.Errorf("%w: platform_control_not_ready", nodeagent.ErrActivationDeferred)

// platformControlAdministration is the coordinator's view of the controller:
// the administration surface it hands to host services, plus the idempotent
// Start it drives from topology changes. Narrowing it here keeps the retry loop
// testable without a real Database Runtime.
type platformControlAdministration interface {
	platformcontrolport.Administration
	Start(context.Context) error
}

type platformControlReplacementReadiness interface {
	AwaitCandidateOpen(context.Context, []string) (bool, error)
}

type platformControlCoordinator struct {
	controller  platformControlAdministration
	replacement platformControlReplacementReadiness
	binding     *sharedstate.BindingStore
	topology    interface {
		SubscribeTopologyChanges() (<-chan struct{}, func())
	}
	logf func(string, ...any)

	mu        sync.Mutex
	lastError string
}

func configurePlatformControlStartup(options reconcileOptions, plane *nodeControlPlane, runtime *nodeagent.ProtocolRuntime, logf func(string, ...any)) (*platformControlCoordinator, error) {
	if options.platformControlProfile == "" {
		return nil, nil
	}
	if runtime == nil {
		return nil, errors.New("Platform Control SQL 模式缺少 Protocol Runtime")
	}
	binding := sharedstate.NewBindingStore()
	coordinator, err := newPlatformControlCoordinator(options, plane, binding, logf)
	if err != nil {
		return nil, err
	}
	runtime.Dependencies.PlatformControl = coordinator.controller
	runtime.ReplacementReadiness = coordinator
	return coordinator, nil
}

func newPlatformControlCoordinator(options reconcileOptions, plane *nodeControlPlane, binding *sharedstate.BindingStore, logf func(string, ...any)) (*platformControlCoordinator, error) {
	if options.platformControlProfile == "" {
		return nil, nil
	}
	if plane == nil || plane.router == nil || binding == nil {
		return nil, errors.New("Platform Control SQL 模式要求 addressing Router 与 Binding Store")
	}
	invoker, err := kernelplatformcontrol.NewAddressingInvoker(plane.router)
	if err != nil {
		return nil, err
	}
	remote, err := platformcontrolport.NewRemoteBootstrapper(invoker)
	if err != nil {
		return nil, err
	}
	credentialsDirectory := options.platformControlCredentialsDirectory
	if credentialsDirectory == "" {
		credentialsDirectory = os.Getenv("CREDENTIALS_DIRECTORY")
	}
	controller, err := kernelplatformcontrol.NewController(
		&kernelplatformcontrol.FileProfileStore{Path: options.platformControlProfile},
		func(ref platformcontrolv1.SecretRef) (platformcontrolport.SecretSource, error) {
			return platformcontrolport.ResolveSecretSource(ref, credentialsDirectory)
		},
		remote,
		binding,
		&kernelplatformcontrol.FileSecretMaterialStore{Root: filepath.Join(filepath.Dir(options.platformControlProfile), "managed-secrets")},
	)
	if err != nil {
		return nil, err
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &platformControlCoordinator{controller: controller, replacement: remote, binding: binding, topology: plane.router, logf: logf}, nil
}

func (c *platformControlCoordinator) Allow(ctx context.Context, unit nodeagent.RuntimeUnit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if unit.StartupTier == "bootstrap" {
		return nil
	}
	// Liveness, not Snapshot: a provider that was bound once but is currently
	// unreachable must still defer activation. Admitting the unit instead turns
	// one recoverable provider outage into a fleet-wide activation failure
	// storm, because every admitted unit fails its first Shared State call and
	// accumulates restart backoff.
	if !c.binding.Live() {
		return errPlatformControlNotReady
	}
	return nil
}

const platformControlReplacementTimeout = 30 * time.Second

func (c *platformControlCoordinator) AwaitReady(ctx context.Context, candidate nodeagent.ReplacementCandidate) error {
	if !candidate.Replacing || candidate.StartupTier != "bootstrap" || c.replacement == nil {
		return nil
	}
	waitCtx, cancel := context.WithTimeout(ctx, platformControlReplacementTimeout)
	defer cancel()
	_, err := c.replacement.AwaitCandidateOpen(waitCtx, candidate.RuntimeInstanceIDs)
	if err != nil {
		return fmt.Errorf("Platform Control 候选副本 Open 未完成: %w", err)
	}
	return nil
}

const (
	platformControlRetryBase = 500 * time.Millisecond
	platformControlRetryMax  = 30 * time.Second
)

// Run is event-driven: topology changes drive Open, and there is no steady
// state poll. Two failure modes still need a floor. A closed subscription (for
// example a rebuilt Router) used to end this loop permanently, and since
// reconcile has no other caller, Open would never be retried again. A reconcile
// that failed while no further topology edge is coming would strand the
// platform the same way. Both are covered by a bounded retry that is armed only
// while something is actually wrong.
func (c *platformControlCoordinator) Run(ctx context.Context) {
	if c == nil {
		return
	}
	backoff := platformControlRetryBase
	for ctx.Err() == nil {
		closed := c.watch(ctx)
		if !closed || ctx.Err() != nil {
			return
		}
		// The subscription ended without the context being cancelled. Re-subscribe
		// so a rebuilt Router does not silently strand bootstrap.
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, platformControlRetryMax)
	}
}

// watch subscribes once and reconciles until the context ends or the
// subscription closes. It reports whether it stopped because the subscription
// closed, which is the only case Run needs to recover from.
func (c *platformControlCoordinator) watch(ctx context.Context) bool {
	updates, cancel := c.topology.SubscribeTopologyChanges()
	defer cancel()

	retry := time.NewTimer(0)
	if !retry.Stop() {
		<-retry.C
	}
	defer retry.Stop()
	backoff := platformControlRetryBase

	arm := func(settled bool) {
		if settled {
			backoff = platformControlRetryBase
			return
		}
		retry.Reset(backoff)
		backoff = min(backoff*2, platformControlRetryMax)
	}
	arm(c.reconcile(ctx))

	for {
		select {
		case <-ctx.Done():
			return false
		case _, ok := <-updates:
			if !ok {
				return true
			}
			if !retry.Stop() {
				select {
				case <-retry.C:
				default:
				}
			}
			arm(c.reconcile(ctx))
		case <-retry.C:
			arm(c.reconcile(ctx))
		}
	}
}

// reconcile reports whether the store settled, so the caller knows whether a
// retry still has to be armed.
func (c *platformControlCoordinator) reconcile(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.controller.Start(ctx); err != nil {
		if ctx.Err() != nil {
			return true
		}
		if err.Error() != c.lastError {
			c.logf("Platform Control Store 尚未就绪: %v", err)
			c.lastError = err.Error()
		}
		return false
	}
	c.lastError = ""
	return true
}

var _ nodeagent.ActivationGate = (*platformControlCoordinator)(nil)
var _ nodeagent.ReplacementReadinessBarrier = (*platformControlCoordinator)(nil)
