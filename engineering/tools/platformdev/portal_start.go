package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func (r *runtime) startPortalKernel(ctx context.Context, env map[string]string, natsURL string, beforePublication func() error) error {
	base, err := r.portalBaseArguments(natsURL)
	if err != nil {
		return err
	}
	if r.options.applyPlatform && r.identity.needsBootstrapPublisher() {
		bootstrapArgs := append(append([]string{}, base...), autoLoginIdentityProtocol{}.portalArguments(r)...)
		if err := r.withBootstrapPublisherPortal(ctx, env, bootstrapArgs, func() error {
			if beforePublication != nil {
				if err := beforePublication(); err != nil {
					return err
				}
			}
			return publishPortal("http://"+r.options.portalListen,
				filepath.Join(r.options.root, "engineering", "deploy", "portal-application-composition.json"),
				filepath.Join(r.options.root, "engineering", "deploy", "portal-platform-catalog.json"))
		}); err != nil {
			return fmt.Errorf("显式发布初始 Portal 组合: %w", err)
		}
	}
	portalArgs := append(append([]string{}, base...), r.identity.portalArguments(r)...)
	if _, err := r.startChild("portal-kernel", env, "node", portalArgs...); err != nil {
		return err
	}
	if err := waitHTTP(ctx, "http://"+r.options.portalListen+"/readyz", 45*time.Second, false); err != nil {
		return fmt.Errorf("Node Portal Kernel 未就绪: %w", err)
	}
	if r.options.applyPlatform && !r.identity.needsBootstrapPublisher() {
		if beforePublication != nil {
			if err := beforePublication(); err != nil {
				return err
			}
		}
		if err := publishPortal("http://"+r.options.portalListen,
			filepath.Join(r.options.root, "engineering", "deploy", "portal-application-composition.json"),
			filepath.Join(r.options.root, "engineering", "deploy", "portal-platform-catalog.json")); err != nil {
			return fmt.Errorf("显式发布初始 Portal 组合: %w", err)
		}
	}
	return nil
}

func (r *runtime) portalBaseArguments(natsURL string) ([]string, error) {
	args := []string{
		filepath.Join(r.options.root, "core", "kernels", "frontend-host", "dist", "portal-host.cjs"),
		"--listen", r.options.portalListen,
		"--allow-insecure-http",
		"--portal-assets", filepath.Join(r.runDir, "portal-assets"),
		"--access-profile-catalog", filepath.Join(r.runDir, "access-profile-catalog.json"),
		"--api-contract-catalog", filepath.Join(r.persistentStateRoot(), "api-contract-catalog.json"),
		"--frontend-delivery-origin", filepath.Join(r.persistentStateRoot(), "frontend-delivery-origin"),
		"--frontend-delivery-cache", filepath.Join(r.runDir, "frontend-delivery-cache"),
		"--nats-servers", natsURL, "--allow-insecure-nats",
		"--addressing-contracts", filepath.Join(r.options.root, "contracts", "proto"),
		"--transport-seed", filepath.Join(r.runDir, "secrets", portalHostTransportSeed),
		"--transport-trust", filepath.Join(r.runDir, "secrets", transportTrustDocument),
		"--composer-logical-service", "platform.portal-composer",
		"--interaction-logical-service", "platform.interaction-broker",
		"--kernel-recovery-url", "http://" + r.options.recoveryListen,
	}
	return appendPublishedAPIExposureCatalog(args, filepath.Join(r.persistentStateRoot(), "api-exposure-gateway.json"))
}

func (r *runtime) withBootstrapPublisherPortal(ctx context.Context, env map[string]string, args []string, action func() error) error {
	cmd := exec.Command("node", args...)
	configureManagedChild(cmd)
	cmd.Dir = r.options.root
	cmd.Env = mergedEnv(env)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动临时 Portal 发布端: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stop := func() error {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(os.Interrupt)
		}
		select {
		case err := <-done:
			return err
		case <-time.After(8 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			return <-done
		}
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = stop()
		}
	}()
	if err := waitHTTPOrExit(ctx, "http://"+r.options.portalListen+"/readyz", 45*time.Second, done); err != nil {
		return fmt.Errorf("临时 Portal 发布端未就绪: %w", err)
	}
	if err := action(); err != nil {
		return err
	}
	if err := stop(); err != nil && !isExpectedProcessStop(err) {
		return fmt.Errorf("关闭临时 Portal 发布端: %w", err)
	}
	stopped = true
	return nil
}

func waitHTTPOrExit(ctx context.Context, endpoint string, timeout time.Duration, done <-chan error) error {
	probe := make(chan error, 1)
	go func() { probe <- waitHTTP(ctx, endpoint, timeout, true) }()
	select {
	case err := <-probe:
		return err
	case err := <-done:
		if err == nil {
			return errors.New("进程在就绪前退出")
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isExpectedProcessStop(err error) bool {
	var exitError *exec.ExitError
	return errors.As(err, &exitError)
}
