package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	kernelplatformcontrol "cdsoft.com.cn/VastPlan/core/shared/go/platformcontrol"
	platformcontrolport "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

var errPlatformControlNotReady = fmt.Errorf("%w: platform_control_not_ready", nodeagent.ErrActivationDeferred)

type platformControlCoordinator struct {
	controller *kernelplatformcontrol.Controller
	binding    *sharedstate.BindingStore
	topology   interface {
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
	// Replace the optional NATS implementation exactly once at the composition
	// root. Downstream host services continue to depend only on Store.
	runtime.Dependencies.SharedState = binding
	runtime.Dependencies.PlatformControl = coordinator.controller
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
	)
	if err != nil {
		return nil, err
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &platformControlCoordinator{controller: controller, binding: binding, topology: plane.router, logf: logf}, nil
}

func (c *platformControlCoordinator) Allow(ctx context.Context, unit nodeagent.RuntimeUnit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if unit.StartupTier == "bootstrap" {
		return nil
	}
	_, _, ready := c.binding.Snapshot()
	if !ready {
		return errPlatformControlNotReady
	}
	return nil
}

// Run is event-driven: it retries Open only when the verified capability
// topology changes. There is no low-frequency bootstrap poll.
func (c *platformControlCoordinator) Run(ctx context.Context) {
	if c == nil {
		return
	}
	updates, cancel := c.topology.SubscribeTopologyChanges()
	defer cancel()
	c.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-updates:
			if !ok {
				return
			}
			c.reconcile(ctx)
		}
	}
}

func (c *platformControlCoordinator) reconcile(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.controller.Start(ctx); err != nil && ctx.Err() == nil {
		if err.Error() != c.lastError {
			c.logf("Platform Control Store 尚未就绪: %v", err)
			c.lastError = err.Error()
		}
		return
	}
	c.lastError = ""
}

var _ nodeagent.ActivationGate = (*platformControlCoordinator)(nil)
